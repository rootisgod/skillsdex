package skillsdex

import (
	"errors"
	"fmt"
	"strings"
)

func runConfig(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: skillsdex config (get default_target|set default_target codex)")
	}
	switch args[0] {
	case "get":
		if len(args) != 2 || args[1] != "default_target" {
			return errors.New("usage: skillsdex config get default_target")
		}
		target, err := resolveTarget("")
		if err != nil {
			return err
		}
		fmt.Println(target)
		return nil
	case "set":
		if len(args) != 3 || args[1] != "default_target" {
			return errors.New("usage: skillsdex config set default_target codex")
		}
		target := strings.TrimSpace(args[2])
		if err := validateTarget(target); err != nil {
			return err
		}
		cfg, err := loadLocalConfig()
		if err != nil {
			return err
		}
		cfg.DefaultTarget = target
		if err := saveLocalConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("default_target = %s\n", target)
		return nil
	default:
		return errors.New("usage: skillsdex config (get default_target|set default_target codex)")
	}
}
