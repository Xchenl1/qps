package request

import "game/component/proto"

type CreateRoomReq struct {
	UnionID    int64          `json:"unionID"` // 1 普通用户创建
	GameRuleId string         `json:"gameRuleId"`
	GameRule   proto.GameRule `json:"gameRule"`
}

type JoinRoomReq struct {
	RoomID string `json:"roomID"`
}
