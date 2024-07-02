package ip

import (
	"context"
	"github.com/cloudflare/cloudflare-go"
	"log"
	"net"
	"time"

	"github.com/mrpk1906/cf-sync/config"
	"go.uber.org/ratelimit"
)

type ManagerSettings struct {
	Limiter           ratelimit.Limiter
	Config            *config.Config
	Logger            *log.Logger
	BackPressureLimit int
}

type Manager struct {
	// our settings
	limiter ratelimit.Limiter
	config  *config.Config
	logger  *log.Logger
	client  *cloudflare.API

	// our presets.
	ipQueue     chan IP
	recordQueue chan cloudflare.DNSRecord
	ipify       IIPify

	// discovered
	upstreamRecords []cloudflare.DNSRecord
}

// NewManager Start a new IP manager.
func NewManager(settings *ManagerSettings) (*Manager, error) {
	ipm := &Manager{
		limiter:     settings.Limiter,
		config:      settings.Config,
		recordQueue: make(chan cloudflare.DNSRecord, settings.BackPressureLimit),
		logger:      settings.Logger,
	}

	var err error
	ipm.client, err = settings.Config.NewClient(settings.Logger)
	if err != nil {
		ipm.logger.Printf("error creating cloudflare client: %s", err)
		return &Manager{}, err
	}

	// try to get the upstream records
	ipm.upstreamRecords, _, err = ipm.client.ListDNSRecords(context.Background(), cloudflare.ResourceIdentifier(ipm.config.ZoneId), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		ipm.logger.Printf("error fetching upstream records: %s", err)
		return &Manager{}, err
	}

	// build the ipify implementation
	ipm.ipQueue = make(chan IP, settings.BackPressureLimit)
	ipm.ipify = NewIPify(&IPifySettings{
		Queue:     ipm.ipQueue,
		Limiter:   settings.Limiter,
		Logger:    settings.Logger,
		CheckIPv6: settings.Config.IpifyCheckIPv6,
	})

	return ipm, nil
}

func (ipm *Manager) Run() {
	ipm.updateRunner()
	ipm.ticker()
}

func (ipm *Manager) Die() {
	ipm.logger.Println("cleaning up before dying.")
	close(ipm.ipQueue)
	close(ipm.recordQueue)
	ipm.logger.Println("she's dead, jim.")
}

func r() {
	if r := recover(); r != nil {
		return
	}
}

// detach the ticker.
func (ipm *Manager) ticker() {
	go func() {
		defer r()
		ticker := time.NewTicker(time.Duration(ipm.config.Frequency) * time.Second)
		for ; true; <-ticker.C {
			ipm.ipify.GetCurrentAddress()
		}
	}()
}

// this is just to facilitate detaching from the request.
func (ipm *Manager) updateRunner() {
	go func() {
		for {
			ipm.updateReceiver(<-ipm.ipQueue)
		}
	}()
}

// now we handle the request.
func (ipm *Manager) updateReceiver(payload IP) {
	for idx := range ipm.config.Records {
		if payload.IsIPv6Available() && ipm.config.Records[idx].Type == "AAAA" {
			ipm.updateAAAARecord(payload.IPv6, ipm.config.Records[idx])
		}
		if ipm.config.Records[idx].Type == "A" {
			ipm.updateARecord(payload.IPv4, ipm.config.Records[idx])
		}
	}
}

func (ipm *Manager) updateARecord(ip net.IP, record cloudflare.DNSRecord) {
	record.Content = ip.String()

	for idx := range ipm.upstreamRecords {
		if ipm.upstreamRecords[idx].Name == record.Name {
			record.ID = ipm.upstreamRecords[idx].ID
		}
	}

	ipm.limiter.Take()
	var err error
	record, err = ipm.client.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(ipm.config.ZoneId), cloudflare.UpdateDNSRecordParams{ID: record.ID, Content: record.Content})
	if err != nil {
		ipm.logger.Printf("error uploading record: %s", err)
		return
	}

	ipm.logger.Printf("updated %s.", record.Name)

}

func (ipm *Manager) updateAAAARecord(ip net.IP, record cloudflare.DNSRecord) {
	record.Content = ip.String()

	for idx := range ipm.upstreamRecords {
		if ipm.upstreamRecords[idx].Name == record.Name {
			record.ID = ipm.upstreamRecords[idx].ID
		}
	}

	ipm.limiter.Take()
	var err error
	record, err = ipm.client.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(ipm.config.ZoneId), cloudflare.UpdateDNSRecordParams{ID: record.ID, Content: record.Content})
	if err != nil {
		ipm.logger.Printf("error uploading record: %s", err)
		return
	}

	ipm.logger.Printf("updated %s", record.Name)
}
