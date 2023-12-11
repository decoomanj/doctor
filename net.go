package health

import (
	"net"
)

// Listener is a net.Listener which stops connections when the health check fails
type Listener struct {
	net.Listener
	health *Doctor
}

// NewListener instantiates a new health listener.
func NewListener(listener net.Listener, health *Doctor) Listener {
	return Listener{
		Listener: listener,
		health:   health,
	}
}

// Accept health aware connections
func (ln Listener) Accept() (c net.Conn, err error) {
	c, err = ln.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// Cleanly close the connection when the service is unhealthy. The server
	// keeps running though until it recovers.
	if !ln.health.Healthy() {
		c.Close()
	}

	// wrap the connection in a connection which can handle timeouts
	return c, nil
}
