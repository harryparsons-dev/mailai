package models

import (
	"time"

	"github.com/uptrace/bun"
)

type UserToken struct {
	bun.BaseModel `bun:"table:user_tokens,alias:ut"`

	Token     string    `bun:",pk" json:"-"`
	UserId    string    `json:"user_id"`
	CreatedAt time.Time `bun:",nullzero,default:current_timestamp" json:"created_at"`
}
