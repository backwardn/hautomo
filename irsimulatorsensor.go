package main

import (
	"log"
	"time"
)

func infraredSimulator(app *Application, key string, stopper *Stopper) {
	defer stopper.Done()

	log.Println("IR simulator: started")

	for {
		select {
		case <-time.After(5 * time.Second):
			app.infraredEvent <- NewInfraredEvent("simulated_remote", key)
		case <-stopper.ShouldStop:
			log.Println("IR simulator: stopping")
			return
		}
	}
}
