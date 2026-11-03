package records

// The catalog (D-171): what a customer is shown about a product — variant, colours, the
// specifications, size and weight, features, warranty, brochure and photos. Every catalog
// entry is of a product that already exists (Products holds each product once, with its
// price); one product can have several entries, one per variant.
func catalogObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "catalog_entries", Module: "catalog", Singular: "Catalog entry", Plural: "Catalog", Icon: "book", Prefix: "CAT",
			Description: "Each product's details for customers: variant, colours, specifications, size, weight, features, warranty, brochure and photos.",
			ObjectBody: ObjectBody{NameLabel: "Catalog entry", StatusLabel: "Status", Statuses: []StatusOption{
				st("active", "Shown", "success"), st("draft", "Being prepared", "neutral"), st("discontinued", "Discontinued", "danger")},
				Fields: []ObjectField{
					{Key: "itemId", Label: "Product", Type: "lookup", Lookup: "catalog_items", Required: true, HelpText: "Pick the product first. Add it under Products if it isn't there."},
					of("variant", "Variant / model", "text"), of("brand", "Brand", "text"), of("modelYear", "Model year", "text"),
					of("colours", "Colours available", "text"), of("engine", "Engine / motor / material", "text"), of("power", "Power / capacity", "text"),
					of("mileage", "Mileage / range", "text"), of("topSpeed", "Top speed", "text"), of("tankCapacity", "Fuel tank / battery", "text"),
					of("dimensions", "Size (L × W × H)", "text"), of("weight", "Weight", "text"), of("seatHeight", "Seat height / ground clearance", "text"),
					of("warranty", "Warranty", "text"), of("brochureUrl", "Brochure link", "url"), of("photos", "Photos", "files"),
					of("features", "Key features", "textarea"), of("specifications", "Full specifications", "textarea")}}},
	}
}
