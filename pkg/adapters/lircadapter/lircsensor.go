package lircadapter

import (
	"bufio"
	"fmt"
	"github.com/function61/gokit/logger"
	"github.com/function61/gokit/stopper"
	"github.com/function61/home-automation-hub/pkg/hapitypes"
	"io"
	"os/exec"
	"regexp"
)

var log = logger.New("lircadapter")

// match lines like this: "000000037ff07bee 00 KEY_VOLUMEDOWN mceusb"

var irParseRe = regexp.MustCompile(" 00 ([a-zA-Z_0-9]+) devinput$")

func irwOutputLineToIrEvent(line string) *hapitypes.InfraredEvent {
	irCommand := irParseRe.FindStringSubmatch(line)
	if irCommand == nil {
		return nil
	}

	return hapitypes.NewInfraredEvent("mceusb", irCommand[1])
}

// reads LIRC's "$ irw" output
func Start(adapter *hapitypes.Adapter, stop *stopper.Stopper) error {
	irw := exec.Command("irw")

	stdoutPipe, err := irw.StdoutPipe()
	if err != nil {
		return err
	}

	if err := irw.Start(); err != nil {
		return err
	}

	go func() {
		bufferedReader := bufio.NewReader(stdoutPipe)

		for {
			// TODO: implement isPrefix
			line, _, err := bufferedReader.ReadLine()
			if err != nil {
				if err == io.EOF {
					return
				}

				panic(err)
			}

			// "000000037ff07bee 00 KEY_VOLUMEDOWN mceusb" => "KEY_VOLUMEDOWN"
			irEvent := irwOutputLineToIrEvent(string(line))
			if irEvent == nil {
				log.Error("mismatched command format")
				continue
			}

			adapter.Receive(irEvent)
		}
	}()

	// TODO: do this via context cancel?
	go func() {
		defer stop.Done()

		log.Info("started")
		defer log.Info("stopped")

		<-stop.Signal

		log.Info("stopping")

		irw.Process.Kill()
	}()

	go func() {
		// wait to complete
		err := irw.Wait()

		log.Error(fmt.Sprintf("$ irw exited, error: %s", err.Error()))
	}()

	return nil
}
