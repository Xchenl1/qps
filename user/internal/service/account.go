package service

import (
	"common/biz"
	"context"
	"core/dao"
	"core/models/entity"
	"core/models/requests"
	"core/repo"
	"framework/myError"
	"time"
	"user/pb"
)

// 创建账号

type AccountService struct {
	accountDao *dao.AccountDao
	redisDao   *dao.RedisDao
	pb.UnimplementedUserServiceServer
}

func NewAccountService(manager *repo.Manager) *AccountService {
	return &AccountService{
		accountDao: dao.NewAccountDao(manager),
		redisDao:   dao.NewRedisDao(manager),
	}
}

func (a *AccountService) Register(ctx context.Context, req *pb.RegisterParams) (*pb.RegisterResponse, error) {
	// 写注册的业务逻辑
	if req.LoginPlatform == requests.WeiXin {
		ac, err := a.wxRegister(req)
		if err != nil {
			return &pb.RegisterResponse{}, myError.GrpcError(err)
		}
		return &pb.RegisterResponse{
			Uid: ac.Uid,
		}, nil
	}

	return &pb.RegisterResponse{}, nil
}

func (a *AccountService) wxRegister(req *pb.RegisterParams) (*entity.Account, *myError.Error) {
	// 1. 封装一个account结构 将其存入数据库 mongo 分布式id objectID
	ac := &entity.Account{
		WxAccount:  req.Account,
		CreateTime: time.Now(),
	}
	// 2. 需要生成几个数字作为用户的唯一id redis自增
	uid, err := a.redisDao.NextAccountId()
	if err != nil {
		return ac, biz.SqlError
	}
	ac.Uid = uid
	err = a.accountDao.SaveAccount(context.TODO(), ac)
	if err != nil {
		return ac, biz.SqlError
	}
	return ac, nil
}
