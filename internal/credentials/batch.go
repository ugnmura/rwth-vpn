package credentials

import (
	"errors"
	"fmt"
	"sort"
)

// Apply writes a confirmed batch. Files use one atomic rename; keyrings roll
// back attempted changes if a write or read-back check fails. Nil deletes a key.
func Apply(store Store, changes map[string]*string) error {
	if file, ok := store.(fileStore); ok {
		return file.apply(changes)
	}
	names := make([]string, 0, len(changes))
	before := map[string]*string{}
	for name := range changes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value, err := store.Get(name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			v := value
			before[name] = &v
		}
	}
	attempted := []string{}
	for _, name := range names {
		attempted = append(attempted, name)
		err := writeValue(store, name, changes[name])
		if err == nil {
			value, readErr := store.Get(name)
			if changes[name] == nil {
				if !errors.Is(readErr, ErrNotFound) {
					err = fmt.Errorf("deletion verification failed for %s", name)
				}
			} else if readErr != nil || value != *changes[name] {
				err = fmt.Errorf("credential verification failed for %s", name)
			}
		}
		if err != nil {
			for i := len(attempted) - 1; i >= 0; i-- {
				if restoreErr := writeValue(store, attempted[i], before[attempted[i]]); restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("restoring %s: %w", attempted[i], restoreErr))
				}
			}
			return err
		}
	}
	return nil
}

func writeValue(store Store, name string, value *string) error {
	if value == nil {
		return store.Delete(name)
	}
	return store.Set(name, *value)
}
