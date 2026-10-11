package uptime

import (
	"context"
	"log"
	"time"
)

// Sampler reports every service's current level. An error means kami couldn't look, which
// says nothing about the services, so that interval goes unrecorded.
type Sampler func(ctx context.Context) ([]Sample, error)

// Run samples every interval until ctx ends, crediting each sample with one interval
func (s *Store) Run(ctx context.Context, sample Sampler, interval time.Duration) {
	tick := func() {
		ctx, cancel := context.WithTimeout(ctx, interval/2)
		defer cancel()
		samples, err := sample(ctx)
		if err != nil {
			log.Printf("uptime: skipping sample: %v", err)
			return
		}
		if err := s.Record(ctx, samples, int64(interval/time.Second)); err != nil {
			log.Printf("uptime: record: %v", err)
		}
	}

	tick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}
