package handler

import (
	"common"
	"common/biz"
	"context"
	"core/repo"
	"core/service"
	"encoding/json"
	"framework/remote"
	"game/logic"
	"game/models/request"
)

type UnionHandler struct {
	um          *logic.UnionManager
	userService *service.UserService
}

func NewUnionHandler(r *repo.Manager, um *logic.UnionManager) *UnionHandler {
	return &UnionHandler{
		um:          um,
		userService: service.NewUserService(r),
	}
}

func (h *UnionHandler) CreateRoom(session *remote.Session, msg []byte) any {
	// union 联盟 持有房间
	// unionManager 管理联盟
	// room 房间 又关联 game接口 实现多个不同的游戏
	// 1. 接收参数
	uid := session.GetUid()
	if len(uid) <= 0 {
		return common.F(biz.InvalidUsers)
	}
	var req request.CreateRoomReq
	if err := json.Unmarshal(msg, &req); err != nil {
		return common.F(biz.RequestDataError)
	}
	// 2. 根据session 用户id 查询用户的信息
	userData, err := h.userService.FindUserByUid(context.TODO(), uid)
	if err != nil {
		return common.F(err)
	}
	if userData == nil {
		return common.F(biz.InvalidUsers)
	}
	// 3. 根据游戏规则 游戏类型 用户信息（创建房间的用户） 创建房间了
	// TODO 需要判断 session中是否已经有roomId， 如果有 代表此用户已经在房间中了，就不能再次创建房间了
	union := h.um.GetUnion(req.UnionID)
	err = union.CreateRoom(h.userService, session, req, userData)
	if err != nil {
		return common.F(err)
	}
	return common.S(nil)
}

func (h *UnionHandler) JoinRoom(session *remote.Session, msg []byte) any {
	uid := session.GetUid()
	if len(uid) <= 0 {
		return common.F(biz.InvalidUsers)
	}
	var req request.JoinRoomReq
	if err := json.Unmarshal(msg, &req); err != nil {
		return common.F(biz.RequestDataError)
	}
	userData, err := h.userService.FindUserByUid(context.TODO(), uid)
	if err != nil {
		return common.F(err)
	}
	if userData == nil {
		return common.F(biz.InvalidUsers)
	}
	bizErr := h.um.JoinRoom(session, req.RoomID, userData)
	if bizErr != nil {
		return common.F(bizErr)
	}
	return common.S(nil)
}
