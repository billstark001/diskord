package gateway

import (
	"net"
	"sync"
)

type trackedListener struct {
	net.Listener
	mu     sync.Mutex
	conns  map[*trackedConn]struct{}
	wg     sync.WaitGroup
	limit  chan struct{}
	closed bool
}
type trackedConn struct {
	net.Conn
	owner *trackedListener
	once  sync.Once
}

func newTracked(l net.Listener, max int) *trackedListener {
	return &trackedListener{Listener: l, conns: make(map[*trackedConn]struct{}), limit: make(chan struct{}, max)}
}
func (l *trackedListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		select {
		case l.limit <- struct{}{}:
		default:
			c.Close()
			continue
		}
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			<-l.limit
			c.Close()
			return nil, net.ErrClosed
		}
		t := &trackedConn{Conn: c, owner: l}
		l.conns[t] = struct{}{}
		l.wg.Add(1)
		l.mu.Unlock()
		return t, nil
	}
}
func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		o := c.owner
		o.mu.Lock()
		delete(o.conns, c)
		o.mu.Unlock()
		<-o.limit
		o.wg.Done()
	})
	return err
}
func (l *trackedListener) CloseAll() {
	l.mu.Lock()
	l.closed = true
	list := make([]*trackedConn, 0, len(l.conns))
	for c := range l.conns {
		list = append(list, c)
	}
	l.mu.Unlock()
	l.Listener.Close()
	for _, c := range list {
		c.Close()
	}
}
