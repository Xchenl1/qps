package dao

import (
	"context"
	"core/models/entity"
	"core/repo"
)

type AccountDao struct {
	repo *repo.Manager
}

// 存储用户
func (d AccountDao) SaveAccount(ctx context.Context, ac *entity.Account) error {
	table := d.repo.Mongo.Db.Collection("account")
	_, err := table.InsertOne(ctx, ac)
	if err != nil {
		return err
	}
	return nil
}

func NewAccountDao(m *repo.Manager) *AccountDao {
	return &AccountDao{
		repo: m,
	}
}
