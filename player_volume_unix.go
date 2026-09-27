//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// setVolumeLive — громкость уже играющего pw-play: своего API у процесса нет,
// поэтому находим его PipeWire-ноду (по приложению и имени файла) и задаём
// channelVolumes через pw-cli. pactl в минимальных сетапах может отсутствовать.
func setVolumeLive(file string, v float64) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return
	}
	var nodes []struct {
		ID   int `json:"id"`
		Info struct {
			Props struct {
				AppName  string `json:"application.name"`
				MediaFN  string `json:"media.filename"`
				NodeName string `json:"node.name"`
			} `json:"props"`
		} `json:"info"`
	}
	if json.Unmarshal(out, &nodes) != nil {
		return
	}
	for _, n := range nodes {
		if n.Info.Props.AppName != "pw-play" && n.Info.Props.NodeName != "pw-play" {
			continue
		}
		if file != "" && n.Info.Props.MediaFN != file && !strings.HasSuffix(n.Info.Props.MediaFN, file) {
			continue
		}
		_ = exec.Command("pw-cli", "s", fmt.Sprint(n.ID), "Props",
			fmt.Sprintf("{ channelVolumes = [ %.3f, %.3f ] }", v, v)).Run()
		return
	}
}
