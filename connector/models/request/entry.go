package request

//	{
//		token:'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIxMDAwMSIsImV4cCI6MTcyOTc1NTYyMn0.rbyGF8q01jYOwVhxoMwhBTBYUBehvBChSh1X510E5JA',
//		userInfo:{nickname:'wx734067',avatar:'Common/head_icon_default',sex:0}
//	}

type EntryReq struct {
	Token    string   `json:"token"`
	UserInfo UserInfo `json:"userInfo"`
}

type UserInfo struct {
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Sex      int    `json:"sex"`
}
