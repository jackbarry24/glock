package core

import (
	"hash/fnv"

	"github.com/google/uuid"
)

func IsValidOwner(uid string) bool {
	if uid == "" {
		return false
	}
	_, err := uuid.Parse(uid)
	return err == nil
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func generateID() string {
	return uuid.New().String()
}
