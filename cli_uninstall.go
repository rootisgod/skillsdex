package skillsdex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func runUninstall(args []string) error {
	identifier, target, confirm, err := parseTargetArgs(args, false)
	if err != nil {
		return errors.New("usage: skillsdex uninstall <owner>/<slug> [--target codex] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	_, in := manifest.find(owner, slug, target)
	if in == nil {
		return errors.New("install is not managed by SkillsDex")
	}
	modified, err := installHasModifiedManagedFiles(*in)
	if err != nil {
		return err
	}
	if modified {
		showPackageDiffs(*in, nil)
		if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Remove it?", in.Path), confirm); err != nil {
			return err
		}
	}
	for _, file := range installedFiles(in) {
		dst, err := installedFileDestination(*in, file.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removeEmptyParents(filepath.Dir(dst), filepath.Dir(in.Path))
	}
	_ = os.Remove(filepath.Dir(in.Path))
	manifest.remove(owner, slug, target)
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Uninstalled %s/%s\n", owner, slug)
	return nil
}
