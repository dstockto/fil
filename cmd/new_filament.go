package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var hexColorPattern = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

var newFilamentCmd = &cobra.Command{
	Use:     "filament",
	Aliases: []string{"fil", "f"},
	Short:   "Create a new filament definition in Spoolman",
	Long:    `Create a new filament definition by specifying name, vendor, material, color, and physical properties.`,
	RunE:    runNewFilament,
}

func runNewFilament(cmd *cobra.Command, _ []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("api endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)
	ctx := cmd.Context()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

	interactive := isInteractiveAllowed(nonInteractive)

	var (
		name        string
		vendorId    int
		vendorName  string
		material    string
		colorHex    string
		price       float64
		density     float64
		diameter    float64
		weight      float64
		spoolWeight float64
		err         error
	)

	if interactive {
		// 1. Filament name
		name, err = promptText("Filament name", "", true)
		if err != nil {
			return err
		}

		// 2. Manufacturer (vendor) selection
		vendorId, vendorName, err = selectOrCreateVendor(cmd, apiClient)
		if err != nil {
			return err
		}

		// 3. Material
		material, err = promptText("Material (e.g. PLA, PETG, ABS)", "", true)
		if err != nil {
			return err
		}

		// 4. Color hex
		colorHex, err = promptColorHex()
		if err != nil {
			return err
		}

		// 5. Price (optional)
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

		// 6. Density
		densityStr, dErr := promptText("Density (g/cm³)", "1.24", false)
		if dErr != nil {
			return dErr
		}
		density, err = strconv.ParseFloat(densityStr, 64)
		if err != nil {
			return fmt.Errorf("invalid density: %w", err)
		}

		// 7. Diameter
		diameterStr, diErr := promptText("Diameter (mm)", "1.75", false)
		if diErr != nil {
			return diErr
		}
		diameter, err = strconv.ParseFloat(diameterStr, 64)
		if err != nil {
			return fmt.Errorf("invalid diameter: %w", err)
		}

		// 8. Weight
		weightStr, wErr := promptText("Net weight (g)", "1000", false)
		if wErr != nil {
			return wErr
		}
		weight, err = strconv.ParseFloat(weightStr, 64)
		if err != nil {
			return fmt.Errorf("invalid weight: %w", err)
		}

		// 9. Spool weight
		spoolWeightStr, swErr := promptText("Spool weight (g)", "140", false)
		if swErr != nil {
			return swErr
		}
		spoolWeight, err = strconv.ParseFloat(spoolWeightStr, 64)
		if err != nil {
			return fmt.Errorf("invalid spool weight: %w", err)
		}
	} else {
		// Non-interactive: read all values from flags
		name, _ = cmd.Flags().GetString("name")
		if name == "" {
			return errors.New("--name is required in non-interactive mode")
		}

		manufacturer, _ := cmd.Flags().GetString("manufacturer")
		material, _ = cmd.Flags().GetString("material")
		colorHex, _ = cmd.Flags().GetString("color")
		price, _ = cmd.Flags().GetFloat64("price")
		density, _ = cmd.Flags().GetFloat64("density")
		diameter, _ = cmd.Flags().GetFloat64("diameter")
		weight, _ = cmd.Flags().GetFloat64("weight")
		spoolWeight, _ = cmd.Flags().GetFloat64("spool-weight")

		if material == "" {
			return errors.New("--material is required in non-interactive mode")
		}

		// Strip leading # from color
		colorHex = strings.TrimPrefix(colorHex, "#")
		if colorHex != "" && !hexColorPattern.MatchString(colorHex) {
			return fmt.Errorf("invalid color hex: %q (expected 6 hex characters)", colorHex)
		}

		// Resolve vendor by name
		if manufacturer != "" {
			vendors, vErr := apiClient.GetVendors(ctx)
			if vErr != nil {
				return fmt.Errorf("failed to fetch vendors: %w", vErr)
			}
			for _, v := range vendors {
				if strings.EqualFold(v.Name, manufacturer) {
					vendorId = v.Id
					vendorName = v.Name
					break
				}
			}
			if vendorId == 0 {
				return fmt.Errorf("vendor %q not found; create it first or use interactive mode", manufacturer)
			}
		}
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

	if dryRun {
		fmt.Println("Dry run — would create filament:")
		fmt.Printf("  Name:         %s\n", req.Name)
		fmt.Printf("  Vendor:       %s (ID %d)\n", vendorName, req.VendorId)
		fmt.Printf("  Material:     %s\n", req.Material)
		fmt.Printf("  Color:        #%s\n", req.ColorHex)
		if req.Price > 0 {
			fmt.Printf("  Price:        %.2f\n", req.Price)
		}
		fmt.Printf("  Density:      %.2f g/cm³\n", req.Density)
		fmt.Printf("  Diameter:     %.2f mm\n", req.Diameter)
		fmt.Printf("  Weight:       %.0f g\n", req.Weight)
		fmt.Printf("  Spool weight: %.0f g\n", req.SpoolWeight)
		return nil
	}

	result, err := apiClient.CreateFilament(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create filament: %w", err)
	}

	fmt.Printf("Created filament #%d: %s %s (%s)\n", result.Id, vendorName, result.Name, result.Material)
	return nil
}

// promptText shows a text prompt with an optional default. If required and empty, it re-prompts.
func promptText(label, defaultVal string, required bool) (string, error) {
	prompt := promptui.Prompt{
		Label:   label,
		Default: defaultVal,
		Stdin:   os.Stdin,
		Stdout:  NoBellStdout,
	}

	for {
		result, err := prompt.Run()
		if err != nil {
			return "", fmt.Errorf("prompt failed: %w", err)
		}
		result = strings.TrimSpace(result)
		if required && result == "" {
			fmt.Println("This field is required. Please enter a value.")
			continue
		}
		return result, nil
	}
}

// promptColorHex prompts for a 6-character hex color code.
func promptColorHex() (string, error) {
	prompt := promptui.Prompt{
		Label:  "Color hex (e.g. FF0000)",
		Stdin:  os.Stdin,
		Stdout: NoBellStdout,
		Validate: func(input string) error {
			cleaned := strings.TrimPrefix(strings.TrimSpace(input), "#")
			if cleaned == "" {
				return fmt.Errorf("color is required")
			}
			if !hexColorPattern.MatchString(cleaned) {
				return fmt.Errorf("must be 6 hex characters (e.g. FF0000)")
			}
			return nil
		},
	}

	result, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("prompt failed: %w", err)
	}

	return strings.TrimPrefix(strings.TrimSpace(result), "#"), nil
}

// selectOrCreateVendor lets the user pick an existing vendor or create a new one.
func selectOrCreateVendor(cmd *cobra.Command, apiClient *api.Client) (int, string, error) {
	ctx := cmd.Context()

	vendors, err := apiClient.GetVendors(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("failed to fetch vendors: %w", err)
	}

	const createNewLabel = "+ Create new vendor"

	// Build display items: "create new" first, then existing vendors
	items := make([]string, 0, len(vendors)+1)
	items = append(items, createNewLabel)
	for _, v := range vendors {
		items = append(items, v.Name)
	}

	searcher := func(input string, index int) bool {
		needle := strings.ToLower(strings.TrimSpace(input))
		if needle == "" {
			return true
		}
		return strings.Contains(strings.ToLower(items[index]), needle)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▸ {{ . | cyan }}",
		Inactive: "  {{ . }}",
		Selected: "✔ {{ . | green }}",
	}

	prompt := promptui.Select{
		Label:             "Select manufacturer",
		Items:             items,
		Templates:         templates,
		Size:              12,
		Searcher:          searcher,
		StartInSearchMode: true,
		Stdin:             os.Stdin,
		Stdout:            NoBellStdout,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return 0, "", fmt.Errorf("vendor selection failed: %w", err)
	}

	if idx == 0 {
		// Create new vendor
		vendorName, pErr := promptText("New vendor name", "", true)
		if pErr != nil {
			return 0, "", pErr
		}

		vendor, cErr := apiClient.CreateVendor(ctx, vendorName)
		if cErr != nil {
			return 0, "", fmt.Errorf("failed to create vendor: %w", cErr)
		}

		fmt.Printf("Created vendor #%d: %s\n", vendor.Id, vendor.Name)
		return vendor.Id, vendor.Name, nil
	}

	// Selected an existing vendor (idx-1 because of the "create new" entry at position 0)
	selected := vendors[idx-1]
	return selected.Id, selected.Name, nil
}

//nolint:gochecknoinits
func init() {
	newCmd.AddCommand(newFilamentCmd)

	newFilamentCmd.Flags().BoolP("dry-run", "d", false, "show what would be created without making API calls")
	newFilamentCmd.Flags().BoolP("non-interactive", "n", false, "disable interactive prompts (requires all flags)")

	// Non-interactive flags
	newFilamentCmd.Flags().String("name", "", "filament name")
	newFilamentCmd.Flags().String("manufacturer", "", "vendor/manufacturer name")
	newFilamentCmd.Flags().String("material", "", "material type (e.g. PLA, PETG)")
	newFilamentCmd.Flags().String("color", "", "color hex code (6 characters, e.g. FF0000)")
	newFilamentCmd.Flags().Float64("price", 0, "price per spool")
	newFilamentCmd.Flags().Float64("density", 1.24, "filament density in g/cm³")
	newFilamentCmd.Flags().Float64("diameter", 1.75, "filament diameter in mm")
	newFilamentCmd.Flags().Float64("weight", 1000, "net filament weight in grams")
	newFilamentCmd.Flags().Float64("spool-weight", 140, "empty spool weight in grams")
}
