// addonName — the human-readable add-on name: the add-on tariff's display name,
// falling back to the add-on's own name so a line is never blank.
export const addonName = (tariffName: string | null | undefined, fallback: string) => tariffName?.trim() || fallback
