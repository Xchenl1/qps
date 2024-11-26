package remote

import (
	"common/logs"
	"encoding/json"
	"framework/protocol"
	"sync"
)

type Session struct {
	sync.RWMutex
	client          Client
	msg             *Msg
	pushChan        chan *userPushMsg
	data            map[string]any
	pushSessionChan chan map[string]any
}

type pushMsg struct {
	data   []byte
	router string
}

type userPushMsg struct {
	PushMsg pushMsg  `json:"pushMsg"`
	Users   []string `json:"users"`
}

func NewSession(client Client, msg *Msg) *Session {
	s := &Session{
		client:          client,
		msg:             msg,
		pushChan:        make(chan *userPushMsg, 1024),
		data:            make(map[string]any),
		pushSessionChan: make(chan map[string]any, 1024),
	}
	go s.pushChanRead()
	go s.pushSessionChanRead()
	return s
}

func (s *Session) GetUid() string {
	return s.msg.Uid
}

func (s *Session) Push(users []string, data any, router string) {
	msg, _ := json.Marshal(data)
	pm := pushMsg{
		data:   msg,
		router: router,
	}
	upm := &userPushMsg{
		Users:   users,
		PushMsg: pm,
	}
	s.pushChan <- upm
}

func (s *Session) pushChanRead() {
	for {
		select {
		case data := <-s.pushChan:
			pushMessage := protocol.Message{
				Type:  protocol.Push,
				ID:    s.msg.Body.ID,
				Route: data.PushMsg.router,
				Data:  data.PushMsg.data,
			}
			msg := Msg{
				Dst:      s.msg.Src,
				Src:      s.msg.Dst,
				Body:     &pushMessage,
				Cid:      s.msg.Cid,
				Uid:      s.msg.Uid,
				PushUser: data.Users,
			}
			result, _ := json.Marshal(msg)
			logs.Info("push message dst:%v", msg.Dst)
			err := s.client.SendMsg(msg.Dst, result)
			if err != nil {
				logs.Error("push message err:%v, msg=%v", err, msg)
			}
		}
	}
}

func (s *Session) Put(key string, value any) {
	s.Lock()
	defer s.Unlock()
	s.data[key] = value
	s.pushSessionChan <- s.data
}

func (s *Session) pushSessionChanRead() {
	for {
		select {
		case data := <-s.pushSessionChan:
			msg := &Msg{
				Dst:         s.msg.Src,
				Src:         s.msg.Dst,
				Cid:         s.msg.Cid,
				Uid:         s.msg.Uid,
				SessionData: data,
				Type:        SessionType,
			}
			res, _ := json.Marshal(msg)
			if err := s.client.SendMsg(msg.Dst, res); err != nil {
				logs.Error("push session data err:%v", err)
			}
		}
	}
}

func (s *Session) SetData(data map[string]any) {
	s.Lock()
	defer s.Unlock()
	for k, v := range data {
		s.data[k] = v
	}
}

func (s *Session) Get(key string) (any, bool) {
	s.RLock()
	defer s.RUnlock()
	v, ok := s.data[key]
	return v, ok
}
