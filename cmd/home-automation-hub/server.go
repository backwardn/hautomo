package main

import (
	"errors"
	"fmt"
	"github.com/function61/gokit/logger"
	"github.com/function61/gokit/ossignal"
	"github.com/function61/gokit/stopper"
	"github.com/function61/home-automation-hub/pkg/hapitypes"
)

var log = logger.New("main")

type Application struct {
	adapterById   map[string]*hapitypes.Adapter
	deviceById    map[string]*hapitypes.Device
	subscriptions map[string]*hapitypes.SubscribeConfig
	inbound       *hapitypes.InboundFabric
}

func NewApplication(stop *stopper.Stopper) *Application {
	app := &Application{
		adapterById:   map[string]*hapitypes.Adapter{},
		deviceById:    map[string]*hapitypes.Device{},
		subscriptions: map[string]*hapitypes.SubscribeConfig{},
		inbound:       hapitypes.NewInboundFabric(),
	}

	go func() {
		defer stop.Done()

		log.Info(fmt.Sprintf("home-automation-hub %s started", version))
		defer log.Info("stopped")

		for {
			select {
			case <-stop.Signal:
				return
			case event := <-app.inbound.Ch:
				app.handleIncomingEvent(event)
			}
		}
	}()

	return app
}

func (a *Application) handleIncomingEvent(inboundEvent hapitypes.InboundEvent) {
	switch e := inboundEvent.(type) {
	case *hapitypes.PersonPresenceChangeEvent:
		log.Info(fmt.Sprintf(
			"Person %s presence changed to %v",
			e.PersonId,
			e.Present))
	case *hapitypes.PowerEvent:
		device := a.deviceById[e.DeviceIdOrDeviceGroupId]

		if err := a.devicePower(device, e); err != nil {
			log.Error(err.Error())
		}
	case *hapitypes.ColorTemperatureEvent:
		device := a.deviceById[e.Device]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewColorTemperatureEvent(
			device.Conf.AdaptersDeviceId,
			e.TemperatureInKelvin))
	case *hapitypes.ColorMsg:
		device := a.deviceById[e.DeviceId]
		adapter := a.adapterById[device.Conf.AdapterId]

		device.LastColor = e.Color

		adapter.Send(hapitypes.NewColorMsg(
			device.Conf.AdaptersDeviceId,
			e.Color))
	case *hapitypes.BrightnessEvent:
		device := a.deviceById[e.DeviceIdOrDeviceGroupId]
		adapter := a.adapterById[device.Conf.AdapterId]

		e2 := hapitypes.NewBrightnessMsg(
			device.Conf.AdaptersDeviceId,
			e.Brightness,
			device.LastColor)
		adapter.Send(&e2)
	case *hapitypes.PlaybackEvent:
		device := a.deviceById[e.DeviceIdOrDeviceGroupId]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewPlaybackEvent(
			device.Conf.AdaptersDeviceId,
			e.Action))
	case *hapitypes.BlinkEvent:
		device := a.deviceById[e.DeviceId]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewBlinkEvent(device.Conf.AdaptersDeviceId))
	case *hapitypes.InfraredEvent:
		a.publish(fmt.Sprintf("infrared:%s:%s", e.Remote, e.Event))
	case *hapitypes.ContactEvent:
		a.publish(fmt.Sprintf("contact:%s:%v", e.Device, e.Contact))
	case *hapitypes.PushButtonEvent:
		a.publish(fmt.Sprintf("pushbutton:%s:%s", e.Device, e.Specifier))
	case *hapitypes.WaterLeakEvent:
		a.publish(fmt.Sprintf("waterleak:%s:%v", e.Device, e.WaterDetected))
	case *hapitypes.HeartbeatEvent:
		a.publish(fmt.Sprintf("heartbeat:%s", e.Device))
	case *hapitypes.TemperatureHumidityPressureEvent:
		fmt.Printf("temp %v\n", e)
	default:
		log.Error("Unsupported inbound event: " + inboundEvent.InboundEventType())
	}
}

func (a *Application) publish(event string) {
	subscription, found := a.subscriptions[event]
	if !found {
		log.Debug(fmt.Sprintf("event %s ignored", event))
		return
	} else {
		log.Debug(fmt.Sprintf("event %s", event))
	}

	for _, action := range subscription.Actions {
		switch action.Verb {
		case "powerOn":
			a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindOn))
		case "powerOff":
			a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindOff))
		case "powerToggle":
			a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindToggle))
		case "blink":
			a.inbound.Receive(hapitypes.NewBlinkEvent(action.Device))
		case "ir":
			device := a.deviceById[action.Device]
			adapter := a.adapterById[device.Conf.AdapterId]

			msg := hapitypes.NewInfraredMsg(action.Device, action.IrCommand)
			adapter.Send(&msg)
		default:
			panic("unknown verb: " + action.Verb)
		}
	}
}

func (a *Application) devicePower(device *hapitypes.Device, power *hapitypes.PowerEvent) error {
	if power.Kind == hapitypes.PowerKindOn {
		log.Debug(fmt.Sprintf("Power on: %s", device.Conf.Name))

		adapter := a.adapterById[device.Conf.AdapterId]
		e := hapitypes.NewPowerMsg(device.Conf.AdaptersDeviceId, device.Conf.PowerOnCmd, true)
		adapter.Send(&e)

		device.ProbablyTurnedOn = true
	} else if power.Kind == hapitypes.PowerKindOff {
		log.Debug(fmt.Sprintf("Power off: %s", device.Conf.Name))

		adapter := a.adapterById[device.Conf.AdapterId]
		e := hapitypes.NewPowerMsg(device.Conf.AdaptersDeviceId, device.Conf.PowerOffCmd, false)
		adapter.Send(&e)

		device.ProbablyTurnedOn = false
	} else if power.Kind == hapitypes.PowerKindToggle {
		log.Debug(fmt.Sprintf("Power toggle: %s, current state = %v", device.Conf.Name, device.ProbablyTurnedOn))

		if device.ProbablyTurnedOn {
			return a.devicePower(device, hapitypes.NewPowerEvent(device.Conf.DeviceId, hapitypes.PowerKindOff))
		} else {
			return a.devicePower(device, hapitypes.NewPowerEvent(device.Conf.DeviceId, hapitypes.PowerKindOn))
		}
	} else {
		return errors.New("unknown power kind")
	}

	return nil
}

func configureAppAndStartAdapters(app *Application, conf *hapitypes.ConfigFile, stopManager *stopper.Manager) error {
	for _, devGroup := range conf.DeviceGroups {
		generatedAdapterId := devGroup.DeviceId + "Group"

		adapterConf := hapitypes.AdapterConfig{
			Id:                 generatedAdapterId,
			Type:               "devicegroup",
			DevicegroupDevices: devGroup.Devices,
		}

		firstDeviceOfGroup := findDeviceConfig(devGroup.Devices[0], conf)
		if firstDeviceOfGroup == nil {
			return fmt.Errorf("device group device not found: %s", devGroup.Devices[0])
		}

		deviceConf := hapitypes.DeviceConfig{
			DeviceId:      devGroup.DeviceId,
			AdapterId:     adapterConf.Id,
			Name:          devGroup.Name,
			Description:   "Device group",
			AlexaCategory: firstDeviceOfGroup.AlexaCategory,
			Type:          firstDeviceOfGroup.Type, // TODO: compute lowest common denominator type?
		}

		conf.Adapters = append(conf.Adapters, adapterConf)
		conf.Devices = append(conf.Devices, deviceConf)
	}

	for _, adapterConf := range conf.Adapters {
		initFn, ok := adapters[adapterConf.Type]
		if !ok {
			return errors.New("unkown adapter: " + adapterConf.Type)
		}

		adapter := hapitypes.NewAdapter(adapterConf, conf, app.inbound)

		if err := initFn(adapter, stopManager.Stopper()); err != nil {
			return err
		}

		app.adapterById[adapter.Conf.Id] = adapter
	}

	for _, deviceConf := range conf.Devices {
		if _, exists := app.deviceById[deviceConf.DeviceId]; exists {
			return fmt.Errorf("duplicate device id %s", deviceConf.DeviceId)
		}

		device := hapitypes.NewDevice(deviceConf)
		app.deviceById[deviceConf.DeviceId] = device
	}

	for _, subscription := range conf.Subscriptions {
		// FIXME: how to do this better?
		tmp := subscription
		app.subscriptions[subscription.Event] = &tmp
	}

	return nil
}

func runServer() error {
	conf, confErr := readConfigurationFile()
	if confErr != nil {
		return confErr
	}

	stopManager := stopper.NewManager()
	defer log.Info("all components stopped")
	defer stopManager.StopAllWorkersAndWait()

	// FIXME: main loop probably shouldn't start here, since there's a race condition
	app := NewApplication(stopManager.Stopper())

	if err := configureAppAndStartAdapters(app, conf, stopManager); err != nil {
		return err
	}

	go handleHttp(conf, stopManager.Stopper())

	log.Info(fmt.Sprintf("stopping due to signal %s", ossignal.WaitForInterruptOrTerminate()))

	return nil
}
