"""Legacy protocol names must not suppress new or correctness findings."""

from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from lint_baseline import anchor, filter_issues


class LintBaselineTests(unittest.TestCase):
    root = Path('/checkout')

    def issue(self, **changes):
        issue = {'FromLinter': 'revive', 'Text': 'var-naming: method GetUrl should be GetURL',
                 'Pos': {'Filename': 'operator/service.go'},
                 'SourceLines': ['func (s *Service) GetUrl(ctx context.Context) error {']}
        issue.update(changes)
        return issue

    def entry(self, issue):
        return {'path': issue['Pos']['Filename'], 'text': issue['Text'],
                'anchor': anchor(issue['SourceLines'][0]), 'count': 1}

    def test_only_the_existing_occurrence_matches(self):
        issue = self.issue()
        failures, matched = filter_issues([issue, issue], [self.entry(issue)], self.root)
        self.assertEqual(matched, 1)
        self.assertEqual(failures, [issue])

    def test_new_or_changed_source_cannot_consume_the_baseline(self):
        issue = self.issue()
        changed = self.issue(SourceLines=['func (s *Service) NewUrl(ctx context.Context) error {'])
        self.assertEqual(filter_issues([changed], [self.entry(issue)], self.root), ([changed], 0))

    def test_correctness_and_core_findings_are_never_suppressed(self):
        for issue in (self.issue(FromLinter='forbidigo'), self.issue(FromLinter='staticcheck'),
                      self.issue(Pos={'Filename': 'execution/service.go'}),
                      self.issue(Pos={'Filename': '../operator/service.go'}),
                      self.issue(Text='unhandled-error: database operation failed')):
            with self.subTest(issue=issue):
                self.assertEqual(filter_issues([issue], [self.entry(issue)], self.root), ([issue], 0))

    def test_absolute_path_and_body_formatting_preserve_protocol_identity(self):
        issue = self.issue()
        formatted = self.issue(Pos={'Filename': '/checkout/operator/service.go'},
                               SourceLines=['func (s *Service) GetUrl(ctx context.Context) error { return nil }'])
        self.assertEqual(filter_issues([formatted], [self.entry(issue)], self.root), ([], 1))


if __name__ == '__main__':
    unittest.main()
