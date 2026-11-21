// Package migrations embeds goose SQL migrations so they can be applied at runtime.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"testing/fstest"
)

// seedFileSuffix — суффикс затравочных миграций, наполняющих таблицы данными.
// Они применяются только при полном запуске приложения (migrate_seed: true);
// при migrate_seed: false база получает чистую схему.
const seedFileSuffix = "_data.sql"

//go:embed *.sql
var FS embed.FS

// SchemaOnly возвращает миграции схемы без затравочных данных.
// goose собирает миграции только glob'ом "*.sql" от корня FS, поэтому
// отфильтрованные файлы копируются в корень MapFS.
func SchemaOnly() (fs.FS, error) {
	matches, err := fs.Glob(FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("glob migrations: %w", err)
	}

	fsys := fstest.MapFS{}
	for _, path := range matches {
		if strings.HasSuffix(path, seedFileSuffix) {
			continue
		}
		data, err := fs.ReadFile(FS, path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", path, err)
		}
		fsys[path] = &fstest.MapFile{Data: data}
	}

	return fsys, nil
}
