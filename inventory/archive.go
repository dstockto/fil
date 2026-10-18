package inventory

import (
	"context"
	"fmt"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
)

// Archiver is the slice of the Spoolman API an archive needs. *api.Client
// satisfies it.
type Archiver interface {
	SettingsReader
	PostSettingObject(ctx context.Context, key string, obj any) error
	FindSpoolsById(ctx context.Context, id int) (*models.FindSpool, error)
	ArchiveSpool(ctx context.Context, spoolId int) error
}

// ArchiveOrders removes ids from every location's order list, then trims the
// empty slots left at the end of a printer location past its capacity — the
// overflow position a bumped spool sat in.
func (l Layout) ArchiveOrders(orders map[string][]int, ids ...int) map[string][]int {
	for _, id := range ids {
		orders = l.RemoveFromAllOrders(orders, id)
	}
	for loc, list := range orders {
		if !l.IsPrinterLocation(loc) {
			continue
		}
		if c := l.Capacity[loc]; c > 0 {
			for len(list) > c && list[len(list)-1] == EmptySlot {
				list = list[:len(list)-1]
			}
			orders[loc] = list
		}
	}
	return orders
}

// Archive archives one spool the way `fil archive <id>` does: it updates
// locations_spoolorders first, then marks the spool archived with no
// location. Unlike the CLI it doesn't ask before archiving a spool that still
// has filament; the caller decides.
//
// Like Move, the read-modify-write of locations_spoolorders is not atomic
// against other writers; callers in one process should serialize.
func Archive(ctx context.Context, sm Archiver, l Layout, id int) (models.FindSpool, error) {
	if id <= 0 {
		return models.FindSpool{}, fmt.Errorf("%w: spool id must be positive", ErrBadRequest)
	}
	spool, err := sm.FindSpoolsById(ctx, id)
	if err != nil {
		return models.FindSpool{}, err
	}
	orders, err := LoadLocationOrders(ctx, sm)
	if err != nil {
		return models.FindSpool{}, err
	}
	orders = l.ArchiveOrders(orders, id)
	if err := sm.PostSettingObject(ctx, "locations_spoolorders", orders); err != nil {
		return models.FindSpool{}, fmt.Errorf("failed to update locations_spoolorders: %w", err)
	}
	if err := sm.ArchiveSpool(ctx, id); err != nil {
		return models.FindSpool{}, fmt.Errorf("error archiving spool #%d: %w", id, err)
	}
	return *spool, nil
}

var _ Archiver = (*api.Client)(nil)
