package room

import (
	"common/logs"
	"core/models/entity"
	"framework/myError"
	"framework/remote"
	"game/component/base"
	"game/component/mj"
	"game/component/proto"
	"game/component/sz"
	"game/models/request"
	"sync"
	"time"
)

type Room struct {
	sync.RWMutex
	Id            string
	unionID       int64
	gameRule      proto.GameRule
	users         map[string]*proto.RoomUser
	RoomCreator   *proto.RoomCreator
	GameFrame     GameFrame
	kickSchedules map[string]*time.Timer
	union         base.UnionBase
	roomDismissed bool
	gameStarted   bool
	askDisMiss    map[int]struct{}
}

func (r *Room) UserReady(uid string, session *remote.Session) {
	r.userReady(uid, session)
}

func (r *Room) EndGame(session *remote.Session) {
	r.gameStarted = false
	for k := range r.users {
		r.users[k].UserStatus = proto.None
	}
}

func (r *Room) UserEntryRoom(session *remote.Session, data *entity.User) *myError.Error {
	r.RoomCreator = &proto.RoomCreator{
		Uid: data.Uid,
	}
	if r.unionID == 1 {
		r.RoomCreator.CreatorType = proto.UserCreatorType
	} else {
		r.RoomCreator.CreatorType = proto.UnionCreatorType
	}
	// 最多6人参加 0-5有6个号
	chairID := r.getEmptyChairID()
	_, ok := r.users[data.Uid]
	if !ok {
		r.users[data.Uid] = proto.ToRoomUser(data, chairID)
	}
	// 2. 将房间号 推送给客户端 更新数据库 当前房间号存储起来
	r.UpdateUserInfoRoomPush(session, data.Uid)
	session.Put("roomId", r.Id)
	// 3. 将游戏类型 推送给客户端 （用户进入游戏的推送）
	r.SelfEntryRoomPush(session, data.Uid)
	// 4. 告诉其他人 此用户进入房间了
	r.OtherUserEntryRoomPush(session, data.Uid)
	go r.addKickScheduleEvent(session, data.Uid)
	return nil
}

func (r *Room) UpdateUserInfoRoomPush(session *remote.Session, uid string) {
	//{roomID: '336842', pushRouter: 'UpdateUserInfoPush'}
	pushMsg := map[string]any{
		"roomID":     r.Id,
		"pushRouter": "UpdateUserInfoPush",
	}
	// node 节点 nats client，push 提供nats将消息发送给connector服务，connector将消息主动发给客户端
	// ServerMessagePush
	session.Push([]string{uid}, pushMsg, "ServerMessagePush")
}

func (r *Room) SelfEntryRoomPush(session *remote.Session, uid string) {
	//{gameType: 1,pushRouter: 'SelfEntryRoomPush'}
	pushMsg := map[string]any{
		"gameType":   r.gameRule.GameType,
		"pushRouter": "SelfEntryRoomPush",
	}
	session.Push([]string{uid}, pushMsg, "ServerMessagePush")
}

func (r *Room) RoomMessageHandle(session *remote.Session, req request.RoomMessageReq) {
	if req.Type == proto.UserReadyNotify {
		r.userReady(session.GetUid(), session)
	}
	if req.Type == proto.GetRoomSceneInfoNotify {
		r.getRoomSceneInfoPush(session)
	}
	if req.Type == proto.AskForDismissNotify {
		r.askForDismiss(session, req.Data.IsExit)
	}
}

func (r *Room) getRoomSceneInfoPush(session *remote.Session) {
	//
	userInfoArr := make([]*proto.RoomUser, 0)
	for _, v := range r.users {
		userInfoArr = append(userInfoArr, v)
	}
	data := map[string]any{
		"type":       proto.GetRoomSceneInfoPush,
		"pushRouter": "RoomMessagePush",
		"data": map[string]any{
			"roomID":          r.Id,
			"roomCreatorInfo": r.RoomCreator,
			"gameRule":        r.gameRule,
			"roomUserInfoArr": userInfoArr,
			"gameData":        r.GameFrame.GetGameData(session),
		},
	}
	session.Push([]string{session.GetUid()}, data, "ServerMessagePush")
}

func (r *Room) addKickScheduleEvent(session *remote.Session, uid string) {
	r.Lock()
	defer r.Unlock()
	t, ok := r.kickSchedules[uid]
	if ok {
		t.Stop()
		delete(r.kickSchedules, uid)
	}
	r.kickSchedules[uid] = time.AfterFunc(30*time.Second, func() {
		logs.Info("kick 定时执行, 代表 用户长时间未准备,uid=%v", uid)
		//取消定时任务
		timer, ok := r.kickSchedules[uid]
		if ok {
			timer.Stop()
		}
		delete(r.kickSchedules, uid)
		//需要判断用户是否该踢出
		user, ok := r.users[uid]
		if ok {
			if user.UserStatus < proto.Ready {
				r.kickUser(user, session)
				// 踢出房间之后，需要判断是否可以解散房间
				if len(r.users) == 0 {
					r.dismissRoom()
				}
			}
		}
	})
}

func (r *Room) ServerMessagePush(users []string, data any, session *remote.Session) {
	session.Push(users, data, "ServerMessagePush")
}
func (r *Room) kickUser(user *proto.RoomUser, session *remote.Session) {
	// 将roomId设为空
	r.ServerMessagePush([]string{user.UserInfo.Uid}, proto.UpdateUserInfoPush(""), session)
	// 通知其他人离开房间
	users := make([]string, 0)
	for _, v := range r.users {
		users = append(users, v.UserInfo.Uid)
	}
	r.ServerMessagePush(users, proto.UserLeaveRoomPushData(user), session)
	delete(r.users, user.UserInfo.Uid)
}

func (r *Room) dismissRoom() {
	if r.TryRLock() {
		r.Lock()
		defer r.Unlock()
	}
	if r.roomDismissed {
		return
	}
	r.roomDismissed = true
	//解散 将union当中存储的room信息 删除掉
	r.cancelAllScheduler()
	r.union.DismissRoom(r.Id)
}

func (r *Room) cancelAllScheduler() {
	// 需要将房间所有的任务 都取消掉
	for uid, v := range r.kickSchedules {
		v.Stop()
		delete(r.kickSchedules, uid)
	}
}

func (r *Room) userReady(uid string, session *remote.Session) {
	//1. push用户的座次,修改用户的状态,取消定时任务
	user, ok := r.users[uid]
	if !ok {
		return
	}
	user.UserStatus = proto.Ready
	timer, ok := r.kickSchedules[uid]
	if ok {
		timer.Stop()
		delete(r.kickSchedules, uid)

	}
	allUsers := r.AllUsers()
	r.ServerMessagePush(allUsers, proto.UserReadyPushData(user.ChairID), session)
	//2. 准备好之后,判断是否需要开始游戏
	if r.IsStartGame() {
		r.startGame(session, user)
	}
}

func (r *Room) JoinRoom(session *remote.Session, data *entity.User) *myError.Error {

	return r.UserEntryRoom(session, data)
}

func (r *Room) OtherUserEntryRoomPush(session *remote.Session, uid string) {
	others := make([]string, 0)
	for _, v := range r.users {
		if v.UserInfo.Uid != uid {
			others = append(others, v.UserInfo.Uid)
		}
	}
	user, ok := r.users[uid]
	if ok {
		r.ServerMessagePush(others, proto.OtherUserEntryRoomPushData(user), session)
	}
}

func (r *Room) AllUsers() []string {
	users := make([]string, 0)
	for _, v := range r.users {
		users = append(users, v.UserInfo.Uid)
	}
	return users
}

func (r *Room) getEmptyChairID() int {
	if len(r.users) == 0 {
		return 0
	}
	r.Lock()
	defer r.Unlock()
	chairID := 0
	for _, v := range r.users {
		if v.ChairID == chairID {
			// 座位号被占用了
			chairID++
		}
	}
	return chairID
}

func (r *Room) IsStartGame() bool {
	// 房间内准备的人数 已经大于等于 最小开始游戏人数
	userReadyCount := 0
	for _, v := range r.users {
		if v.UserStatus == proto.Ready {
			userReadyCount++
		}
	}
	if r.gameRule.GameType == int(proto.HongZhong) {
		if len(r.users) == userReadyCount && userReadyCount >= r.gameRule.MaxPlayerCount {
			return true
		}
	}
	if len(r.users) == userReadyCount && userReadyCount >= r.gameRule.MinPlayerCount {
		return true
	}
	return false
}

func (r *Room) startGame(session *remote.Session, user *proto.RoomUser) {
	if r.gameStarted {
		return
	}
	r.gameStarted = true
	for _, v := range r.users {
		v.UserStatus = proto.Playing
	}
	r.GameFrame.StartGame(session, user)
}

func NewRoom(id string, unionID int64, rule proto.GameRule, u base.UnionBase) *Room {
	r := &Room{
		Id:            id,
		unionID:       unionID,
		gameRule:      rule,
		users:         make(map[string]*proto.RoomUser),
		kickSchedules: make(map[string]*time.Timer),
		union:         u,
	}
	if rule.GameType == int(proto.PinSanZhang) {
		r.GameFrame = sz.NewGameFrame(rule, r)
	}
	if rule.GameType == int(proto.HongZhong) {
		r.GameFrame = mj.NewGameFrame(rule, r)
	}
	return r
}

func (r *Room) GetUsers() map[string]*proto.RoomUser {
	return r.users
}
func (r *Room) GetId() string {
	return r.Id
}
func (r *Room) GameMessageHandle(session *remote.Session, msg []byte) {
	// 需要游戏去处理具体的消息
	user, ok := r.users[session.GetUid()]
	if !ok {
		return
	}
	r.GameFrame.GameMessageHandle(user, session, msg)
}

func (r *Room) askForDismiss(session *remote.Session, exit bool) {
	r.Lock()
	defer r.Unlock()
	//所有同意座次的数组
	if exit {
		//同意解散
		if r.askDisMiss == nil {
			r.askDisMiss = make(map[int]struct{})
		}
		user := r.users[session.GetUid()]
		r.askDisMiss[user.ChairID] = struct{}{}

		nameArr := make([]string, len(r.users))
		chairIDArr := make([]any, len(r.users))
		avatarArr := make([]string, len(r.users))
		onlineArr := make([]bool, len(r.users))
		for _, v := range r.users {
			nameArr[v.ChairID] = v.UserInfo.Nickname
			avatarArr[v.ChairID] = v.UserInfo.Avatar
			_, ok := r.askDisMiss[v.ChairID]
			if ok {
				chairIDArr[v.ChairID] = true
			}
			onlineArr[v.ChairID] = true
		}
		data := proto.DismissPushData{
			NameArr:    nameArr,
			ChairIDArr: chairIDArr,
			AskChairId: user.ChairID,
			OnlineArr:  onlineArr,
			AvatarArr:  avatarArr,
			Tm:         30,
		}
		r.sendData(proto.AskForDismissPushData(&data), session)
		if len(r.askDisMiss) == len(r.users) {
			//所有人都同意 解散
			for _, v := range r.users {
				r.kickUser(v, session)
			}
			if len(r.users) == 0 {
				r.dismissRoom()
			}
		}
	} else {
		user := r.users[session.GetUid()]
		//不同意解散
		nameArr := make([]string, len(r.users))
		chairIDArr := make([]any, len(r.users))
		avatarArr := make([]string, len(r.users))
		onlineArr := make([]bool, len(r.users))
		for _, v := range r.users {
			nameArr[v.ChairID] = v.UserInfo.Nickname
			avatarArr[v.ChairID] = v.UserInfo.Avatar
			_, ok := r.askDisMiss[v.ChairID]
			if ok {
				chairIDArr[v.ChairID] = true
			}
			onlineArr[v.ChairID] = true
		}
		data := proto.DismissPushData{
			NameArr:    nameArr,
			ChairIDArr: chairIDArr,
			AskChairId: user.ChairID,
			OnlineArr:  onlineArr,
			AvatarArr:  avatarArr,
			Tm:         30,
		}
		r.sendData(proto.AskForDismissPushData(&data), session)
	}
}

func (r *Room) sendData(data any, session *remote.Session) {
	r.ServerMessagePush(r.AllUsers(), data, session)
}
