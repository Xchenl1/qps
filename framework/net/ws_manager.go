package net

import (
	"common/logs"
	"common/utils"
	"encoding/json"
	"errors"
	"fmt"
	"framework/game"
	"framework/protocol"
	"framework/remote"
	"github.com/gorilla/websocket"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	websocketUpgrade = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

type CheckOriginHandler func(r *http.Request) bool

type Manager struct {
	sync.RWMutex
	websocketUpgrade   *websocket.Upgrader
	ServerId           string
	CheckOriginHandler CheckOriginHandler
	clients            map[string]Connection
	ClientReadChan     chan *MsgPack
	handlers           map[protocol.PackageType]EventHandler
	ConnectorHandlers  LogicHandler
	RemoteReadChan     chan []byte      // 连接nats
	RemoteCli          remote.Client    // nats
	RemotePushChan     chan *remote.Msg // nats
}

type HandlerFuc func(session *Session, body []byte) (any, error)
type LogicHandler map[string]HandlerFuc
type EventHandler func(packet *protocol.Packet, c Connection) error

func (m *Manager) Run(addr string) {
	go m.clientReadChanHandler()
	go m.remoteReadChanHandler()
	go m.remotePushChanHandler()
	http.HandleFunc("/", m.serveWS)
	// 设置不同的消息处理器
	m.setupEventHandlers()
	logs.Fatal("connector listen serve err:%v", http.ListenAndServe(addr, nil))
}

func (m *Manager) serveWS(w http.ResponseWriter, r *http.Request) {
	// websocket 基于http
	if m.websocketUpgrade == nil {
		m.websocketUpgrade = &websocketUpgrade
	}
	wsConn, err := m.websocketUpgrade.Upgrade(w, r, nil)
	if err != nil {
		logs.Error("websocketUpgrade.Upgrade err:%v", err)
		return
	}
	client := NewWsConnection(wsConn, m)
	m.addClient(client)
	client.Run()
}

func (m *Manager) addClient(client *WsConnection) {
	m.Lock()
	defer m.Unlock()
	m.clients[client.Cid] = client
}

func (m *Manager) removeClient(wc WsConnection) {
	for cid, c := range m.clients {
		if cid == wc.Cid {
			c.Close()
			delete(m.clients, cid)
		}
	}
}

func (m *Manager) clientReadChanHandler() {
	for {
		select {
		case body, ok := <-m.ClientReadChan:
			if ok {
				m.decodeClientPack(body)
			}
		}
	}
}

func (m *Manager) decodeClientPack(body *MsgPack) {
	// 解析协议
	//logs.Info("receiver message:%v", string(body.Body))
	packet, err := protocol.Decode(body.Body)
	if err != nil {
		logs.Error("decode message err:%v", err)
		return
	}
	if err := m.routeEvent(packet, body.Cid); err != nil {
		logs.Error("routeEvent err:%v", err)
	}
}

func (m *Manager) Close() {
	for cid, v := range m.clients {
		v.Close()
		delete(m.clients, cid)
	}
}

func (m *Manager) routeEvent(packet *protocol.Packet, cid string) error {
	// 根据packet.type来做不同的处理 处理器
	conn, ok := m.clients[cid]
	if ok {
		handler, ok := m.handlers[packet.Type]
		if ok {
			return handler(packet, conn)
		} else {
			return errors.New("no packetType found")
		}
	}
	return errors.New("no client found")
}

func (m *Manager) setupEventHandlers() {
	m.handlers[protocol.Handshake] = m.HandshakeHandler
	m.handlers[protocol.HandshakeAck] = m.HandshakeAckHandler
	m.handlers[protocol.Heartbeat] = m.HeartbeatHandler
	m.handlers[protocol.Data] = m.MessageHandler
	m.handlers[protocol.Kick] = m.KickHandler
}

func (m *Manager) HandshakeHandler(packet *protocol.Packet, c Connection) error {
	res := protocol.HandshakeResponse{
		Code: 200,
		Sys: protocol.Sys{
			Heartbeat: 3,
		},
	}
	data, _ := json.Marshal(res)
	buf, err := protocol.Encode(packet.Type, data)
	if err != nil {
		logs.Error("encode packet err:%v", err)
		return err
	}
	return c.SendMessage(buf)
}

func (m *Manager) HandshakeAckHandler(packet *protocol.Packet, c Connection) error {
	logs.Info("receiver handshake ack message")
	return nil
}

func (m *Manager) HeartbeatHandler(packet *protocol.Packet, c Connection) error {
	logs.Info("receiver heartbeat message:%v", packet.Type)
	var res []byte
	data, _ := json.Marshal(res)
	buf, err := protocol.Encode(packet.Type, data)
	if err != nil {
		logs.Error("encode packet err:%v", err)
		return err
	}
	return c.SendMessage(buf)
}

func (m *Manager) MessageHandler(packet *protocol.Packet, c Connection) error {
	message := packet.MessageBody()
	logs.Info("receiver message body, type=%v, route=%v, data:%v",
		message.Type, message.Route, string(message.Data))
	// connector.entryHandler.entry
	routeStr := message.Route
	routers := strings.Split(routeStr, ".")
	if len(routers) != 3 {
		return errors.New("router unsupported")
	}
	serverType := routers[0]
	handlerMethod := fmt.Sprintf("%s.%s", routers[1], routers[2])
	connectorConfig := game.Conf.GetConnectorByServerType(serverType)
	if connectorConfig != nil {
		// 本地connector服务处理器
		handler, ok := m.ConnectorHandlers[handlerMethod]
		if ok {
			data, err := handler(c.GetSession(), message.Data)
			if err != nil {
				return err
			}
			marshal, _ := json.Marshal(data)
			message.Type = protocol.Response
			message.Data = marshal
			encode, err := protocol.MessageEncode(message)
			if err != nil {
				return err
			}
			res, err := protocol.Encode(packet.Type, encode)
			if err != nil {
				return err
			}
			return c.SendMessage(res)
		}
	} else {
		//nats 远端调用处理 hall.userHandler.updateUserAddress
		dst, err := m.selectDst(serverType)
		if err != nil {
			logs.Error("remote send msg selectDst err:%v", err)
			return err
		}
		msg := &remote.Msg{
			Cid:         c.GetSession().Cid,
			Uid:         c.GetSession().Uid,
			Src:         m.ServerId,
			Dst:         dst,
			Router:      handlerMethod,
			Body:        message,
			SessionData: c.GetSession().data,
		}
		data, _ := json.Marshal(msg)
		err = m.RemoteCli.SendMsg(dst, data)
		if err != nil {
			logs.Error("remote send msg err:%v", err)
			return err
		}
	}
	return nil
}

func (m *Manager) KickHandler(packet *protocol.Packet, c Connection) error {
	logs.Info("receiver kick message")
	return nil
}

func (m *Manager) remoteReadChanHandler() {
	for {
		select {
		case body, ok := <-m.RemoteReadChan:
			if ok {
				logs.Info("sub nats msg:%v", string(body))
				var msg remote.Msg
				if err := json.Unmarshal(body, &msg); err != nil {
					logs.Error("nats remote message format err:%v", err)
					continue
				}
				if msg.Type == remote.SessionType {
					// 需要特殊处理，session类型是存储在connection中的session 并不推送客户端
					m.setSessionData(msg)
					continue
				}
				if msg.Body != nil {
					if msg.Body.Type == protocol.Request || msg.Body.Type == protocol.Response {
						// 给客户端回信息 都是 response
						msg.Body.Type = protocol.Response
						m.Response(&msg)
					}
					if msg.Body.Type == protocol.Push {
						m.RemotePushChan <- &msg
					}
				}
			}
		}
	}
}

func (m *Manager) selectDst(serverType string) (string, error) {
	serversConfigs, ok := game.Conf.ServersConf.TypeServer[serverType]
	if !ok {
		return "", errors.New("no server found")
	}
	// 随机一个 比较好的一个策略
	rand.New(rand.NewSource(time.Now().UnixNano()))
	index := rand.Intn(len(serversConfigs))
	return serversConfigs[index].ID, nil
}

func (m *Manager) Response(msg *remote.Msg) {
	connection, ok := m.clients[msg.Cid]
	if !ok {
		logs.Info("%s client down, uid=%s", msg.Cid, msg.Uid)
		return
	}
	buf, err := protocol.MessageEncode(msg.Body)
	if err != nil {
		logs.Error("Response MessageEncode err:%v", err)
		return
	}
	res, err := protocol.Encode(protocol.Data, buf)
	if err != nil {
		logs.Error("Response Encode err:%v", err)
		return
	}
	if msg.Body.Type == protocol.Push {
		for _, v := range m.clients {
			if utils.Contains(msg.PushUser, v.GetSession().Uid) {
				v.SendMessage(res)
			}
		}
	} else {
		connection.SendMessage(res)
	}
}

func (m *Manager) remotePushChanHandler() {
	for {
		select {
		case body, ok := <-m.RemotePushChan:
			if ok {
				logs.Info("nats push message:%v", body)
				if body.Body.Type == protocol.Push {
					m.Response(body)
				}
			}
		}
	}
}

func (m *Manager) setSessionData(msg remote.Msg) {
	m.RLock()
	defer m.RUnlock()
	connection, ok := m.clients[msg.Cid]
	if ok {
		connection.GetSession().SetData(msg.Uid, msg.SessionData)
	}
}

func NewManager() *Manager {
	return &Manager{
		ClientReadChan: make(chan *MsgPack, 1024),
		clients:        make(map[string]Connection),
		handlers:       make(map[protocol.PackageType]EventHandler),
		RemoteReadChan: make(chan []byte, 1024),
		RemotePushChan: make(chan *remote.Msg, 1024),
	}
}
