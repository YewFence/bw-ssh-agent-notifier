After changing Go code, run `mise run check` before finishing, make sure the check passes. If the check fails, try to run `mise run fix` to fix the issues automatically. If the check still fails, you may need to fix the issues manually.

This project is in early development and does not require backward compatibility yet. When a cleaner long-term design requires an incompatible change, make the change deliberately instead of preserving compatibility through extra complexity.
