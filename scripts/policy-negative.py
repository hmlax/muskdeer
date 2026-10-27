#!/usr/bin/env python3
"""Prove configured lint policies reject violations and accept model metadata."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
NEGATIVE = '''// Package negative contains deliberately forbidden lint fixtures.
package negative
import (
 "context"

 dbalias "gorm.io/gorm"
 decalias "github.com/shopspring/decimal"
)
type databaseAlias = dbalias.DB
// Forbidden deliberately violates the checked conventions without executing SQL.
func Forbidden(ctx context.Context, session *databaseAlias) {
 borrowed := session.WithContext(ctx)
 choose := borrowed.Table
 _ = choose("forbidden_probe").Error
 _ = decalias.NewFromFloat(1)
 var rate float64
 _ = rate
 _ = borrowed.Exec("SELECT 1").Error
 _ = borrowed.AutoMigrate()
}
'''
POSITIVE = '''// Package positive verifies allowed generated-model operations.
package positive
import (
 "context"

 "gorm.io/gorm"

 modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)
// Allowed binds a generated model; metadata inspection does not select a table.
func Allowed(ctx context.Context, database *gorm.DB) error {
 row := &modelv1.StorageProbe{}
 statement := &gorm.Statement{DB: database}
 if err := statement.Parse(row); err != nil { return err }
 _ = statement.Schema.Table
 return database.WithContext(ctx).Model(&modelv1.StorageProbe{}).First(row).Error
}
'''


def check():
    version = json.loads((ROOT / 'tools/versions.json').read_text())['golangci_lint']
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    environment = os.environ.copy()
    environment['GOLANGCI_LINT_CACHE'] = str(cache / 'golangci-lint')
    for source, expected in [(NEGATIVE, False), (POSITIVE, True)]:
        with tempfile.TemporaryDirectory(prefix='policy-lint-fixture-', dir=cache) as directory:
            path = Path(directory)
            (path / 'fixture.go').write_text(source)
            subprocess.run([
                sys.executable, str(ROOT / 'scripts/go.py'), 'fmt', './' + str(path.relative_to(ROOT)),
            ], cwd=ROOT, env=environment, check=True, capture_output=True, text=True, timeout=30)
            result = subprocess.run([
                sys.executable, str(ROOT / 'scripts/go.py'), 'run',
                'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@' + version,
                'run', '--config', str(ROOT / '.golangci.yml'), './' + str(path.relative_to(ROOT)),
            ], cwd=ROOT, env=environment, capture_output=True, text=True, timeout=300)
            output = result.stdout + result.stderr
            if expected and result.returncode != 0:
                raise ValueError('valid generated-model fixture failed: ' + output)
            messages = ['string table selection is forbidden', 'float intermediates are forbidden', 'Use decimal.Decimal', 'Raw SQL is confined', 'Use explicit versioned migrations']
            if not expected and (result.returncode == 0 or not all(message in output for message in messages)):
                raise ValueError('invalid lint fixtures were not rejected by their intended rules: ' + output)
    print('Lint negative aliases/method values/floats rejected; generated Model/metadata control passed')


if __name__ == '__main__':
    try:
        check()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
