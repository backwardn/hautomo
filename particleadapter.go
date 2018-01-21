package main

import (
	"log"
)

func NewParticleAdapter(id string, particleId string, accessToken string) *Adapter {
	adapter := NewAdapter(id)

	go func() {
		log.Println("ParticleAdapter: started")

		for {
			select {
			case powerMsg := <-adapter.PowerMsg:
				if accessToken == "" {
					log.Printf("ParticleAdapter: error: PARTICLE_ACCESS_TOKEN not defined")
					continue
				}
				if err := particleRequest(particleId, "rf", powerMsg.PowerCommand, accessToken); err != nil {
					log.Printf("ParticleAdapter: request failed: %s", err.Error())
				}
			}
		}
	}()

	return adapter
}
