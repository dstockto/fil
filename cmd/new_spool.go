package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var newSpoolCmd = &cobra.Command{
	Use:     "spool",
	Aliases: []string{"sp", "s"},
	Short:   "Create new spool(s) in Spoolman",
	Long:    `Create one or more new spools by selecting an existing filament or creating a new one.`,
	RunE:    runNewSpool,
}

func runNewSpool(cmd *cobra.Command, _ []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("api endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	ctx := cmd.Context()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

	interactive := isInteractiveAllowed(nonInteractive)

	var (
		filamentId   int
		filamentName string
		price        float64
		quantity     int
		location     string
		err          error
	)

	if interactive {
		// 1. Select or create filament
		filamentId, filamentName, err = selectOrCreateFilament(cmd, apiClient)
		if err != nil {
			return err
		}

		// 2. Price (optional)
		priceStr, pErr := promptText("Price (leave empty to skip)", "", false)
		if pErr != nil {
			return pErr
		}
		if priceStr != "" {
			price, err = strconv.ParseFloat(priceStr, 64)
			if err != nil {
				return fmt.Errorf("invalid price: %w", err)
			}
		}

		// 3. Location (optional)
		location, err = promptText("Location (leave empty to skip)", "", false)
		if err != nil {
			return err
		}
		if location != "" {
			location = MapToAlias(location)
		}

		// 4. Quantity
		for {
			qtyStr, qErr := promptText("Quantity", "1", false)
			if qErr != nil {
				return qErr
			}
			if qtyStr == "" {
				quantity = 1
				break
			}
			quantity, err = strconv.Atoi(qtyStr)
			if err != nil || quantity < 1 {
				fmt.Println("Please enter a valid number (1 or more).")
				continue
			}
			break
		}
	} else {
		// Non-interactive: read all values from flags
		filamentId, _ = cmd.Flags().GetInt("filament-id")
		if filamentId == 0 {
			return errors.New("--filament-id is required in non-interactive mode")
		}
		filamentName = fmt.Sprintf("filament #%d", filamentId)

		price, _ = cmd.Flags().GetFloat64("price")
		quantity, _ = cmd.Flags().GetInt("quantity")
		if quantity < 1 {
			quantity = 1
		}
		location, _ = cmd.Flags().GetString("location")
		if location != "" {
			location = MapToAlias(location)
		}
	}

	req := models.CreateSpoolRequest{
		FilamentId: filamentId,
		Price:      price,
		Location:   location,
	}

	if dryRun {
		fmt.Printf("Dry run — would create %d spool(s):\n", quantity)
		fmt.Printf("  Filament:  %s (ID %d)\n", filamentName, req.FilamentId)
		if req.Price > 0 {
			fmt.Printf("  Price:     %.2f\n", req.Price)
		}
		if req.Location != "" {
			fmt.Printf("  Location:  %s\n", req.Location)
		}
		fmt.Printf("  Quantity:  %d\n", quantity)
		return nil
	}

	var created []models.FindSpool
	for i := range quantity {
		spool, cErr := apiClient.CreateSpool(ctx, req)
		if cErr != nil {
			return fmt.Errorf("failed to create spool %d of %d: %w", i+1, quantity, cErr)
		}
		created = append(created, *spool)
	}

	spoolWord := "spool"
	if quantity > 1 {
		spoolWord = "spools"
	}
	fmt.Printf("Created %d %s:\n", len(created), spoolWord)
	for _, s := range created {
		fmt.Printf(" - %s\n", s)
	}

	// Update locations_spoolorders if the new spools have a location
	if location != "" {
		orders, oErr := LoadLocationOrders(ctx, apiClient)
		if oErr == nil {
			for _, s := range created {
				list := orders[location]
				if IsPrinterLocation(location) {
					emptyIdx := FirstEmptySlot(list)
					if emptyIdx >= 0 {
						list[emptyIdx] = s.Id
					} else {
						list = append(list, s.Id)
					}
				} else {
					list = append(list, s.Id)
				}
				orders[location] = list
			}
			_ = apiClient.PostSettingObject(ctx, "locations_spoolorders", orders)
		}
	}

	return nil
}

// selectOrCreateFilament lets the user pick an existing filament or create a new one.
func selectOrCreateFilament(cmd *cobra.Command, apiClient *api.Client) (int, string, error) {
	ctx := cmd.Context()

	filaments, err := apiClient.GetFilaments(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("failed to fetch filaments: %w", err)
	}

	const createNewLabel = "+ Create new filament"

	type filamentItem struct {
		label string
		id    int
	}

	items := make([]filamentItem, 0, len(filaments)+1)
	items = append(items, filamentItem{label: createNewLabel, id: 0})
	for _, f := range filaments {
		label := fmt.Sprintf("%s %s (%s)", f.Vendor.Name, f.Name, f.Material)
		items = append(items, filamentItem{label: label, id: f.Id})
	}

	displayItems := make([]string, len(items))
	for i, it := range items {
		displayItems[i] = it.label
	}

	searcher := func(input string, index int) bool {
		if index == 0 {
			return true // always show "+ Create new filament"
		}
		needle := strings.ToLower(strings.TrimSpace(input))
		if needle == "" {
			return true
		}
		return strings.Contains(strings.ToLower(displayItems[index]), needle)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▸ {{ . | cyan }}",
		Inactive: "  {{ . }}",
		Selected: "✔ {{ . | green }}",
	}

	prompt := promptui.Select{
		Label:             "Select filament",
		Items:             displayItems,
		Templates:         templates,
		Size:              12,
		Searcher:          searcher,
		StartInSearchMode: true,
		Stdin:             os.Stdin,
		Stdout:            NoBellStdout,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return 0, "", fmt.Errorf("filament selection failed: %w", err)
	}

	if idx == 0 {
		// Create new filament — delegate to the existing filament creation workflow
		result, cErr := runNewFilamentInteractive(cmd, apiClient)
		if cErr != nil {
			return 0, "", cErr
		}
		vendorName := result.Vendor.Name
		label := fmt.Sprintf("%s %s (%s)", vendorName, result.Name, result.Material)
		return result.Id, label, nil
	}

	selected := items[idx]
	return selected.id, selected.label, nil
}

// runNewFilamentInteractive runs the interactive filament creation flow and returns the created filament.
func runNewFilamentInteractive(cmd *cobra.Command, apiClient *api.Client) (*models.FilamentResponse, error) {
	ctx := cmd.Context()

	name, err := promptText("Filament name", "", true)
	if err != nil {
		return nil, err
	}

	vendorId, _, err := selectOrCreateVendor(cmd, apiClient)
	if err != nil {
		return nil, err
	}

	material, err := promptText("Material (e.g. PLA, PETG, ABS)", "", true)
	if err != nil {
		return nil, err
	}

	colorHex, err := promptColorHex()
	if err != nil {
		return nil, err
	}

	priceStr, pErr := promptText("Filament price (leave empty to skip)", "", false)
	if pErr != nil {
		return nil, pErr
	}
	var price float64
	if priceStr != "" {
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid price: %w", err)
		}
	}

	densityStr, dErr := promptText("Density (g/cm³)", "1.24", false)
	if dErr != nil {
		return nil, dErr
	}
	density, err := strconv.ParseFloat(densityStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid density: %w", err)
	}

	diameterStr, diErr := promptText("Diameter (mm)", "1.75", false)
	if diErr != nil {
		return nil, diErr
	}
	diameter, err := strconv.ParseFloat(diameterStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid diameter: %w", err)
	}

	weightStr, wErr := promptText("Net weight (g)", "1000", false)
	if wErr != nil {
		return nil, wErr
	}
	weight, err := strconv.ParseFloat(weightStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid weight: %w", err)
	}

	spoolWeightStr, swErr := promptText("Spool weight (g)", "140", false)
	if swErr != nil {
		return nil, swErr
	}
	spoolWeight, err := strconv.ParseFloat(spoolWeightStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid spool weight: %w", err)
	}

	req := models.CreateFilamentRequest{
		Name:        name,
		VendorId:    vendorId,
		Material:    material,
		Price:       price,
		Density:     density,
		Diameter:    diameter,
		Weight:      weight,
		SpoolWeight: spoolWeight,
		ColorHex:    colorHex,
	}

	result, err := apiClient.CreateFilament(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create filament: %w", err)
	}

	fmt.Printf("Created filament #%d: %s %s (%s)\n", result.Id, result.Vendor.Name, result.Name, result.Material)
	return result, nil
}

//nolint:gochecknoinits
func init() {
	newCmd.AddCommand(newSpoolCmd)

	newSpoolCmd.Flags().BoolP("dry-run", "d", false, "show what would be created without making API calls")
	newSpoolCmd.Flags().BoolP("non-interactive", "n", false, "disable interactive prompts (requires all flags)")

	// Non-interactive flags
	newSpoolCmd.Flags().Int("filament-id", 0, "filament ID to use for the spool")
	newSpoolCmd.Flags().Float64("price", 0, "price per spool")
	newSpoolCmd.Flags().Int("quantity", 1, "number of spools to create")
	newSpoolCmd.Flags().StringP("location", "l", "", "location for the new spool(s)")
}
