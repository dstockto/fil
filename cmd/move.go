/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// moveCmd represents the move command.
var moveCmd = &cobra.Command{
	Use:     "move",
	Short:   "Moves a spool to a new location",
	Long:    `Moves a spool to a new location.`,
	RunE:    runMove,
	Aliases: []string{"mv", "m", "mov"},
}

type move struct {
	spoolId int
	spool   models.FindSpool
	from    string
	to      string
	err     error
}

func (m move) String() string {
	if m.err != nil {
		return fmt.Sprintf("not moving %d: %s", m.spoolId, m.err)
	}

	return fmt.Sprintf("Moving #%d from %s to %s", m.spoolId, m.from, m.to)
}

func runMove(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}

	// allFrom is a search limiter so we don't need to have an exact location
	allFrom, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}

	// allTo is a destination location which should be an exact location, so we'll try aliasing to allow for shortcuts
	allTo, err := cmd.Flags().GetString("destination")
	if err != nil {
		return err
	}

	allTo = mapToAlias(allTo)

	var (
		errs  error
		moves []move
	)

	// Each individual argument needs to correspond to one spool or one location (if allTo is not specified).
	// If we have more than one, then it's an error

	for i := 0; i < len(args); i++ {
		spoolSelector := args[i]

		destination := allTo
		if destination == "" {
			// if allTo was not set, then the next argument should be the destination
			if i+1 < len(args) {
				destination = args[i+1]
				destination = mapToAlias(destination)
				i++
			} else {
				errs = errors.Join(errs, errors.New("destination must be specified if not using --destination/-d"))

				break
			}
		}

		// figure out if we can get a spool from the selector
		// first try to get a spool by ID
		spoolId := -1

		id, iderr := strconv.Atoi(spoolSelector)
		if iderr == nil {
			spoolId = id
		}

		if spoolId == -1 {
			query := make(map[string]string)
			if allFrom != "" {
				query["location"] = allFrom
			}

			spools, lookupErr := apiClient.FindSpoolsByName(spoolSelector, nil, query)
			if lookupErr != nil {
				errs = errors.Join(errs, errors.New("error looking up spool: "+lookupErr.Error()))

				continue
			}

			if len(spools) == 0 {
				errs = errors.Join(errs, fmt.Errorf("spool not found: %s", spoolSelector))

				continue
			}

			if len(spools) != 1 {
				errs = errors.Join(errs, fmt.Errorf("multiple spools found (%d): %s", len(spools), spoolSelector))

				continue
			}

			spoolId = spools[0].Id
		}

		moves = append(moves, move{
			spoolId: spoolId,
			to:      destination,
		})
	}

	// If we get here, we have a list of moves to make, we need to check that they exist, figure out where we're moving
	// from
	for i, m := range moves {
		// check that the spool exists
		spool, findErr := apiClient.FindSpoolsById(m.spoolId)
		if errors.Is(findErr, api.ErrSpoolNotFound) {
			theErr := fmt.Errorf("spool #%d not found", m.spoolId)
			errs = errors.Join(errs, theErr)
			moves[i].err = theErr

			continue
		}

		if findErr != nil {
			theErr := fmt.Errorf("error finding spool: %w", findErr)
			errs = errors.Join(errs, theErr)
			moves[i].err = theErr

			continue
		}

		moves[i].from = spool.Location
		moves[i].spool = *spool
	}

	// Error checking is done, now we can actually move the spools
	if dryRun {
		fmt.Println("Dry run:")
	}

	for _, m := range moves {
		// We just print the error moves
		if m.err != nil {
			fmt.Printf("Skipping due to error - %s\n", m)

			continue
		}

		to := m.to
		if m.to == "<empty>" {
			to = "nowhere"
		}

		if dryRun {
			fmt.Printf("Moving %s to %s\n", m.spool, to)

			continue
		}

		moveErr := apiClient.MoveSpool(m.spoolId, m.to)
		if moveErr != nil {
			color.Red("Error moving spool %s: %v\n", m.spool, moveErr)
			errs = errors.Join(errs, fmt.Errorf("error moving spool %s: %w", m.spool, moveErr))

			continue
		}

		fmt.Printf("Moving %s to %s\n", m.spool, to)
	}

	cmd.SilenceUsage = true

	return errs
}

// mapToAlias maps a location alias to a location name. If it's not found in the map, it returns the original string.
func mapToAlias(to string) string {
	aliasMap := Cfg.LocationAliases
	if aliasMap == nil {
		return to
	}

	if val, ok := aliasMap[strings.ToUpper(to)]; ok {
		return val
	}

	return to
}

func init() {
	rootCmd.AddCommand(moveCmd)

	moveCmd.Flags().Bool("dry-run", false, "show what would be moved, but don't actually move anything")
	moveCmd.Flags().StringP("destination", "d", "", "destination for all spools")
	moveCmd.Flags().StringP("from", "f", "", "source location for all spools")
	// add flag for the "nowhere" location, or maybe use a special name or alias?
}
