package route

import (
	"core/repo"
	"framework/node"
	"game/handler"
	"game/logic"
)

// Register 业务逻辑
func Register(r *repo.Manager) node.LogicHandler {
	handlers := make(node.LogicHandler)
	um := logic.NewUnionManager()
	unionHandler := handler.NewUnionHandler(r, um)
	handlers["unionHandler.createRoom"] = unionHandler.CreateRoom
	handlers["unionHandler.joinRoom"] = unionHandler.JoinRoom
	gameHandler := handler.NewGameHandler(r, um)
	handlers["gameHandler.roomMessageNotify"] = gameHandler.RoomMessageNotify
	handlers["gameHandler.gameMessageNotify"] = gameHandler.GameMessageNotify
	// 后台gm工具
	gmHandler := handler.NewGmHandler(r)
	handlers["gm.modifyUserInfo"] = gmHandler.ModifyUserInfo
	handlers["gm.modifyTime"] = gmHandler.ModifyTime
	return handlers
}
