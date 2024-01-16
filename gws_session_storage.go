package remotedialer

import "sync"

func newSMap() *sMap { return &sMap{data: make(map[string]*any)} }

type sMap struct {
	sync.RWMutex
	data map[string]*any
}

func (c *sMap) Len() int {
	c.RLock()
	defer c.RUnlock()
	return len(c.data)
}

func (c *sMap) Load(key string) (value any, exist bool) {
	c.RLock()
	defer c.RUnlock()
	value, exist = c.data[key]
	return
}

func (c *sMap) Delete(key string) {
	c.Lock()
	defer c.Unlock()
	delete(c.data, key)
}

func (c *sMap) Store(key string, value any) {
	c.Lock()
	defer c.Unlock()
	c.data[key] = &value
}

func (c *sMap) Range(f func(key string, value any) bool) {
	c.RLock()
	defer c.RUnlock()

	for k, v := range c.data {
		if !f(k, v) {
			return
		}
	}
}
