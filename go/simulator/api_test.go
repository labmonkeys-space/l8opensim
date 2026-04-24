package main

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestAPIServerStopClosesHTTPServer(t *testing.T) {
	originalListen := apiListenTCP
	originalNewServer := apiNewHTTPServer
	t.Cleanup(func() {
		apiListenTCP = originalListen
		apiNewHTTPServer = originalNewServer
	})

	listener := &stubListener{}
	server := newBlockingAPIServer()
	apiListenTCP = func(device *DeviceSimulator, addr string) (net.Listener, error) {
		return listener, nil
	}
	apiNewHTTPServer = func(handler http.Handler) apiHTTPServer {
		return server
	}

	device := &DeviceSimulator{
		IP:      net.IPv4(127, 0, 0, 1),
		APIPort: 8443,
		resources: &DeviceResources{
			API: []APIResource{
				{
					Method:   "GET",
					Path:     "/health",
					Response: map[string]string{"status": "ok"},
				},
			},
		},
	}
	apiServer := &APIServer{
		device:        device,
		sharedTLSCert: &tls.Certificate{},
	}

	if err := apiServer.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	<-server.serveCalled

	if err := apiServer.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	select {
	case <-server.closeCalled:
	default:
		t.Fatal("Stop() did not close the HTTP server")
	}
}

type blockingAPIServer struct {
	serveCalled chan struct{}
	closeCalled chan struct{}
	done        chan struct{}
}

func newBlockingAPIServer() *blockingAPIServer {
	return &blockingAPIServer{
		serveCalled: make(chan struct{}),
		closeCalled: make(chan struct{}),
		done:        make(chan struct{}),
	}
}

func (s *blockingAPIServer) Serve(net.Listener) error {
	close(s.serveCalled)
	<-s.done
	return http.ErrServerClosed
}

func (s *blockingAPIServer) Close() error {
	close(s.closeCalled)
	close(s.done)
	return nil
}

type stubListener struct{}

func (l *stubListener) Accept() (net.Conn, error) {
	return nil, errors.New("not implemented")
}

func (l *stubListener) Close() error { return nil }

func (l *stubListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8443}
}
