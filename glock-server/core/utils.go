package core

import (
	"github.com/google/uuid"
)

func IsValidOwner(uid string) bool {
	if uid == "" {
		return false
	}
	_, err := uuid.Parse(uid)
	return err == nil
}
