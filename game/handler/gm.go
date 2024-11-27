package handler

import (
	"core/repo"
	"core/service"
	"framework/remote"
)

type GmHandler struct {
	userService *service.UserService
}

func NewGmHandler(r *repo.Manager) *GmHandler {
	return &GmHandler{
		userService: service.NewUserService(r),
	}
}

// ModifyUserInfo 修改玩家信息
func (gm *GmHandler) ModifyUserInfo(session *remote.Session, msg []byte) any {
	return nil
}

// ModifyTime 修改时间
func (gm *GmHandler) ModifyTime(session *remote.Session, msg []byte) any {
	return nil
}
