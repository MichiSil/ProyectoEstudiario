package cache

import "time"

type Noop struct{}

func NewNoop() *Noop {
	return &Noop{}
}

func (n *Noop) Name() string {
	return "none"
}

func (n *Noop) Get(string) ([]byte, error) {
	return nil, ErrCacheMiss
}

func (n *Noop) Set(string, []byte, time.Duration) error {
	return nil
}

func (n *Noop) Delete(string) error {
	return nil
}
