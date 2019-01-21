package main

import (
	"errors"
	"fmt"
	"github.com/function61/gokit/dynversion"
	"github.com/function61/gokit/jsonfile"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/stopper"
	"github.com/function61/home-automation-hub/pkg/hapitypes"
	"github.com/function61/home-automation-hub/pkg/suntimes"
	"log"
	"time"
)

const statefilePath = "state-snapshot.json"

type Application struct {
	adapterById   map[string]*hapitypes.Adapter
	deviceById    map[string]*hapitypes.Device
	subscriptions map[string]*hapitypes.SubscribeConfig
	inbound       *hapitypes.InboundFabric
	booleans      *booleanStorage
	logl          *logex.Leveled
}

func NewApplication(logger *log.Logger, stop *stopper.Stopper) *Application {
	app := &Application{
		adapterById:   map[string]*hapitypes.Adapter{},
		deviceById:    map[string]*hapitypes.Device{},
		subscriptions: map[string]*hapitypes.SubscribeConfig{},
		inbound:       hapitypes.NewInboundFabric(),
		booleans:      NewBooleanStorage("anybodyHome", "environmentHasLight"),
		logl:          logex.Levels(logger),
	}

	app.booleans.Set("anybodyHome", true)
	app.updateEnvironmentLightStatus(false)

	everyMinute := time.NewTicker(1 * time.Minute)
	go func() {
		defer stop.Done()

		app.logl.Info.Printf("home-automation-hub %s started", dynversion.Version)
		defer app.logl.Info.Println("stopped")

		for {
			select {
			case <-stop.Signal:
				if err := app.saveStateSnapshot(); err != nil {
					app.logl.Error.Printf("failed saving state on shutting down: %v", err)
				}
				return
			case <-everyMinute.C:
				app.updateEnvironmentLightStatus(true)

				if err := app.saveStateSnapshot(); err != nil {
					app.logl.Error.Printf("failed saving state: %v", err)
				}
			case event := <-app.inbound.Ch:
				app.handleIncomingEvent(event)
			}
		}
	}()

	return app
}

func (a *Application) updateEnvironmentLightStatus(broadcastChanges bool) {
	hasLight := suntimes.IsBetweenGoldenHours(time.Now(), suntimes.Tampere)
	changed, _ := a.booleans.Set("environmentHasLight", hasLight)
	if changed && broadcastChanges {
		a.logl.Info.Printf("environmentHasLight changed to %v", hasLight)
	}
}

func (a *Application) saveStateSnapshot() error {
	statefile := hapitypes.NewStatefile()

	for _, device := range a.deviceById {
		snap, err := device.SnapshotState()
		if err != nil {
			return err
		}

		statefile.Devices[device.Conf.DeviceId] = *snap
	}

	return jsonfile.Write(statefilePath, &statefile)
}

func (a *Application) handleIncomingEvent(inboundEvent hapitypes.InboundEvent) {
	switch e := inboundEvent.(type) {
	case *hapitypes.PersonPresenceChangeEvent:
		a.logl.Info.Printf(
			"Person %s presence changed to %v",
			e.PersonId,
			e.Present)
	case *hapitypes.PowerEvent:
		device := a.deviceById[e.DeviceIdOrDeviceGroupId]

		if err := a.devicePower(device, e); err != nil {
			a.logl.Error.Println(err.Error())
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
	case *hapitypes.PublishEvent:
		a.publish(e.Topic)
	case *hapitypes.BrightnessEvent:
		device := a.deviceById[e.DeviceIdOrDeviceGroupId]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewBrightnessMsg(
			device.Conf.AdaptersDeviceId,
			e.Brightness,
			device.LastColor))
	case *hapitypes.PlaybackEvent:
		device := a.deviceById[e.Device]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewPlaybackEvent(
			device.Conf.AdaptersDeviceId,
			e.Action))
	case *hapitypes.BlinkEvent:
		device := a.deviceById[e.DeviceId]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewBlinkEvent(device.Conf.AdaptersDeviceId))
	case *hapitypes.InfraredEvent:
		device := a.deviceById[e.Device]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewInfraredEvent(device.Conf.AdaptersDeviceId, e.Command))
	case *hapitypes.RawInfraredEvent:
		a.publish(fmt.Sprintf("infrared:%s:%s", e.Remote, e.Event))
	case *hapitypes.ContactEvent:
		a.updateLastOnline(e.Device)
		a.publish(fmt.Sprintf("contact:%s:%v", e.Device, e.Contact))
	case *hapitypes.PushButtonEvent:
		a.updateLastOnline(e.Device)
		a.publish(fmt.Sprintf("pushbutton:%s:%s", e.Device, e.Specifier))
	case *hapitypes.WaterLeakEvent:
		a.updateLastOnline(e.Device)
		a.publish(fmt.Sprintf("waterleak:%s:%v", e.Device, e.WaterDetected))
	case *hapitypes.LinkQualityEvent:
		a.updateLastOnline(e.Device)

		device := a.deviceById[e.Device]
		device.LinkQuality = e.LinkQuality
	case *hapitypes.BatteryStatusEvent:
		a.updateLastOnline(e.Device)

		device := a.deviceById[e.Device]
		device.BatteryPct = e.BatteryPct
		device.BatteryVoltage = e.Voltage
	case *hapitypes.TemperatureHumidityPressureEvent:
		device := a.deviceById[e.Device]
		device.LastTemperatureHumidityPressureEvent = e

		a.updateLastOnline(e.Device)
	default:
		a.logl.Error.Printf("Unsupported inbound event: " + inboundEvent.InboundEventType())
	}
}

func (a *Application) updateLastOnline(deviceId string) {
	device := a.deviceById[deviceId]
	now := time.Now()
	device.LastOnline = &now
}

func (a *Application) publish(event string) {
	subscription, found := a.subscriptions[event]
	if !found {
		a.logl.Debug.Printf("event %s ignored", event)
		return
	} else {
		a.logl.Debug.Printf("event %s", event)
	}

	for _, condition := range subscription.Conditions {
		switch condition.Type {
		case "boolean-not-changed-within":
			lastChange, err := a.booleans.GetLastChangeTime(condition.Boolean)
			if err != nil {
				a.logl.Error.Printf("error evaluating condition: %v", err)
				return
			}

			if time.Since(lastChange).Seconds() < float64(condition.DurationSeconds) {
				a.logl.Debug.Printf(
					"boolean %s changed within %d seconds - bailing out",
					condition.Boolean,
					condition.DurationSeconds)
				return
			}
		case "boolean-is-false":
			fallthrough
		case "boolean-is-true":
			val, err := a.booleans.Get(condition.Boolean)
			if err != nil {
				a.logl.Error.Printf("error evaluating condition: %v", err)
				return
			}

			expectedValue := condition.Type == "boolean-is-true"

			if val != expectedValue {
				a.logl.Debug.Printf(
					"bool %s expected %v but got %v - bailing out",
					condition.Boolean,
					expectedValue,
					val)
				return
			}
		}
	}

	for _, action := range subscription.Actions {
		if err := a.runAction(action); err != nil {
			a.logl.Error.Printf("failure running action: %v", err)
		}
	}
}

func (a *Application) runAction(action hapitypes.ActionConfig) error {
	switch action.Verb {
	case "sleep":
		time.Sleep(time.Duration(action.DurationSeconds) * time.Second)
	case "powerOn":
		a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindOn))
	case "powerOff":
		a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindOff))
	case "powerToggle":
		a.inbound.Receive(hapitypes.NewPowerEvent(action.Device, hapitypes.PowerKindToggle))
	case "blink":
		a.inbound.Receive(hapitypes.NewBlinkEvent(action.Device))
	case "setBooleanTrue":
		fallthrough
	case "setBooleanFalse":
		value := action.Verb == "setBooleanTrue"
		changed, err := a.booleans.Set(action.Boolean, value)
		if err != nil {
			return err
		}

		if changed {
			if value {
				a.publish(fmt.Sprintf("boolean:%s:changes-to-true", action.Boolean))
			} else {
				a.publish(fmt.Sprintf("boolean:%s:changes-to-false", action.Boolean))
			}
		}
	case "ir":
		device := a.deviceById[action.Device]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewInfraredMsg(
			device.Conf.AdaptersDeviceId,
			action.IrCommand))
	case "playback":
		device := a.deviceById[action.Device]
		adapter := a.adapterById[device.Conf.AdapterId]

		adapter.Send(hapitypes.NewPlaybackEvent(
			device.Conf.AdaptersDeviceId,
			action.PlaybackAction))
	default:
		return fmt.Errorf("unknown verb: %s", action.Verb)
	}

	return nil
}

func (a *Application) devicePower(device *hapitypes.Device, power *hapitypes.PowerEvent) error {
	if power.Kind == hapitypes.PowerKindOn {
		a.logl.Debug.Printf("Power on: %s", device.Conf.Name)

		adapter := a.adapterById[device.Conf.AdapterId]
		adapter.Send(hapitypes.NewPowerMsg(
			device.Conf.AdaptersDeviceId,
			device.Conf.PowerOnCmd,
			true))

		device.ProbablyTurnedOn = true

		a.publish(fmt.Sprintf("device:%s:power:on", device.Conf.DeviceId))
	} else if power.Kind == hapitypes.PowerKindOff {
		a.logl.Debug.Printf("Power off: %s", device.Conf.Name)

		adapter := a.adapterById[device.Conf.AdapterId]
		adapter.Send(hapitypes.NewPowerMsg(
			device.Conf.AdaptersDeviceId,
			device.Conf.PowerOffCmd,
			false))

		device.ProbablyTurnedOn = false

		a.publish(fmt.Sprintf("device:%s:power:off", device.Conf.DeviceId))
	} else if power.Kind == hapitypes.PowerKindToggle {
		a.logl.Debug.Printf("Power toggle: %s, current state = %v", device.Conf.Name, device.ProbablyTurnedOn)

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

func configureAppAndStartAdapters(
	app *Application,
	conf *hapitypes.ConfigFile,
	logger *log.Logger,
	stopManager *stopper.Manager,
) error {
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

		adapter := hapitypes.NewAdapter(
			adapterConf,
			conf,
			app.inbound,
			logex.Prefix(adapterConf.Id, logger))

		if err := initFn(adapter, stopManager.Stopper()); err != nil {
			return err
		}

		app.adapterById[adapter.Conf.Id] = adapter
	}

	statefile := hapitypes.NewStatefile()
	if err := jsonfile.Read(statefilePath, &statefile, true); err != nil {
		return err
	}

	for _, deviceConf := range conf.Devices {
		if _, exists := app.deviceById[deviceConf.DeviceId]; exists {
			return fmt.Errorf("duplicate device id %s", deviceConf.DeviceId)
		}

		snapshot, snapshotFound := statefile.Devices[deviceConf.DeviceId]
		if !snapshotFound {
			snapshot = hapitypes.DeviceStateSnapshot{
				ProbablyTurnedOn: false,
				LastColor:        hapitypes.RGB{Red: 255, Green: 255, Blue: 255},
			}
		}

		device, err := hapitypes.NewDevice(deviceConf, snapshot)
		if err != nil {
			return err
		}
		app.deviceById[deviceConf.DeviceId] = device
	}

	for _, subscription := range conf.Subscriptions {
		if _, exists := app.subscriptions[subscription.Event]; exists {
			return fmt.Errorf(
				"two subscriptions for event not yet supported; event: %s",
				subscription.Event)
		}

		// FIXME: how to do this better?
		tmp := subscription
		app.subscriptions[subscription.Event] = &tmp
	}

	return nil
}

func runServer(logger *log.Logger, stop *stopper.Stopper) error {
	defer stop.Done()

	logl := logex.Levels(logger)

	conf, confErr := readConfigurationFile()
	if confErr != nil {
		return confErr
	}

	workers := stopper.NewManager()

	defer logl.Info.Println("all components stopped")

	// FIXME: main loop probably shouldn't start here, since there's a race condition
	app := NewApplication(logex.Prefix("hub", logger), workers.Stopper())

	if err := configureAppAndStartAdapters(app, conf, logger, workers); err != nil {
		return err
	}

	go handleHttp(app, conf, logex.Prefix("handleHttp", logger), workers.Stopper())

	<-stop.Signal

	workers.StopAllWorkersAndWait()

	return nil
}
