package ip

import (
	"io"
	"log"
	"net"
	"net/http"

	"go.uber.org/ratelimit"
)

// IIPify How do we want to interact with IPify?
type IIPify interface {
	GetCurrentAddress()
}

// IPify How we are interacting with IPify
type IPify struct {
	c         chan IP
	logger    *log.Logger
	limiter   ratelimit.Limiter
	checkIPv6 bool
}

// IPifySettings Our settings.
type IPifySettings struct {
	Queue     chan IP
	Limiter   ratelimit.Limiter
	Logger    *log.Logger
	CheckIPv6 bool
}

// NewIPify Build a new IPify implementation
func NewIPify(settings *IPifySettings) *IPify {
	return &IPify{
		c:         settings.Queue,
		limiter:   settings.Limiter,
		logger:    settings.Logger,
		checkIPv6: settings.CheckIPv6,
	}
}

func (ipy *IPify) GetCurrentAddress() {
	ipy.logger.Println("refreshing public ip.")

	var ipRef IP

	ipy.limiter.Take()
	resp4, err := http.Get("https://api.ipify.org")
	if err != nil {
		ipy.logger.Fatalf("cannot get ip: %s", err)
		return
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != http.StatusOK {
		ipy.logger.Printf("cannot read response from ipify, response code: %d", resp4.StatusCode)
		return
	}

	var body []byte
	body, err = io.ReadAll(resp4.Body)
	if err != nil {
		ipy.logger.Fatalf("cannot read ipify response: %s", err)
		return
	}

	ipRef.IPv4 = net.ParseIP(string(body))
	ipy.logger.Printf("current public ipv4 is %s.", ipRef.IPv4)

	if err = resp4.Body.Close(); err != nil {
		ipy.logger.Fatal(err)
		return
	}

	if ipy.checkIPv6 {
		ipy.limiter.Take()
		var resp6 *http.Response
		resp6, err = http.Get("https://api6.ipify.org")
		if err != nil {
			ipy.logger.Printf("cannot get ip: %s", err)
			return
		}
		defer resp6.Body.Close()

		if resp6.StatusCode != http.StatusOK {
			ipy.logger.Printf("cannot read response from ipify, response code: %d", resp6.StatusCode)
		}

		body, err = io.ReadAll(resp6.Body)
		if err != nil {
			ipy.logger.Fatalf("cannot read ipify response: %s", err)
			return
		}

		ipRef.IPv6 = net.ParseIP(string(body))

		if ipRef.IsIPv6Available() {
			ipy.logger.Printf("current public ipv6 is %s.", ipRef.IPv6)
		}
	}

	if err = resp4.Body.Close(); err != nil {
		ipy.logger.Fatal(err)
		return
	}

	ipy.c <- ipRef
}
