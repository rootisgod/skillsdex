package skillsdex

import (
	"errors"
	"fmt"
	"strings"
)

func runMark(args []string) error {
	opts, err := parseMarkArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex mark <owner>/<slug> [--pinned|--unpinned] [--recommended|--not-recommended] [--apply|--dry-run] [--yes|--no]")
	}
	owner, slug, err := parseIdentifier(opts.Identifier)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	current := resp.Skill
	nextPinned := current.PinnedInstall
	nextRecommended := current.RecommendedDefault
	if opts.Pinned != nil {
		nextPinned = *opts.Pinned
	}
	if opts.Recommended != nil {
		nextRecommended = *opts.Recommended
	}
	changes := skillFlagChanges(current, nextPinned, nextRecommended)
	identifier := current.OwnerSlug + "/" + current.Slug
	if len(changes) == 0 {
		fmt.Printf("%s already has pinned=%s, recommended=%s.\n", identifier, yesNo(current.PinnedInstall), yesNo(current.RecommendedDefault))
		return nil
	}
	if !opts.Apply {
		fmt.Printf("Would update %s install flags:\n", identifier)
		for _, change := range changes {
			fmt.Println("  " + change)
		}
		fmt.Println("Next steps:")
		fmt.Printf("  %s\n", markApplyCommand(identifier, opts))
		return nil
	}
	if err := requireConfirmation("Update install flags for "+identifier+"?", opts.Confirm); err != nil {
		return err
	}
	updated, err := client.updateSkillFlags(owner, slug, SkillFlagUpdate{PinnedInstall: &nextPinned, RecommendedDefault: &nextRecommended})
	if err != nil {
		return err
	}
	fmt.Printf("Updated %s: pinned=%s, recommended=%s.\n", updated.OwnerSlug+"/"+updated.Slug, yesNo(updated.PinnedInstall), yesNo(updated.RecommendedDefault))
	return nil
}

func skillFlagChanges(current Skill, nextPinned, nextRecommended bool) []string {
	var changes []string
	if current.PinnedInstall != nextPinned {
		changes = append(changes, fmt.Sprintf("Pinned install: %s -> %s", yesNo(current.PinnedInstall), yesNo(nextPinned)))
	}
	if current.RecommendedDefault != nextRecommended {
		changes = append(changes, fmt.Sprintf("Recommended default: %s -> %s", yesNo(current.RecommendedDefault), yesNo(nextRecommended)))
	}
	return changes
}

func markApplyCommand(identifier string, opts markOptions) string {
	parts := []string{"skillsdex", "mark", shellQuote(identifier)}
	if opts.Pinned != nil {
		if *opts.Pinned {
			parts = append(parts, "--pinned")
		} else {
			parts = append(parts, "--unpinned")
		}
	}
	if opts.Recommended != nil {
		if *opts.Recommended {
			parts = append(parts, "--recommended")
		} else {
			parts = append(parts, "--not-recommended")
		}
	}
	parts = append(parts, "--apply", "--yes")
	return strings.Join(parts, " ")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
