package identity

import (
	"crypto/rand"
	"fmt"
	"io"
)

type IDGenerator interface {
	NewID() (string, error)
}

type UUIDv4Generator struct {
	random io.Reader
}

func NewUUIDv4Generator(random io.Reader) *UUIDv4Generator {
	if random == nil {
		random = rand.Reader
	}
	return &UUIDv4Generator{random: random}
}

func (g *UUIDv4Generator) NewID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(g.random, value); err != nil {
		return "", ErrRepository
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
