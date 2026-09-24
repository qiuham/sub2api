import re
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]


def read_yaml(name):
    return yaml.safe_load((ROOT / name).read_text())


def events(name):
    data = read_yaml('.github/workflows/' + name)
    return data.get('on', data.get(True, {}))


class ReleasePolicyTest(unittest.TestCase):
    def test_short_version_has_positive_revision(self):
        self.assertIsNotNone(re.fullmatch(r'\d+\.\d+\.\d+-N\.[1-9]\d*', (ROOT / 'backend/cmd/server/VERSION').read_text().strip()))

    def test_security_scan_does_not_duplicate_each_push(self):
        self.assertEqual(set(events('security-scan.yml')), {'schedule', 'workflow_dispatch'})

    def test_code_ci_is_one_run_per_event(self):
        event = events('backend-ci.yml')
        self.assertEqual(event['push']['branches'], ['main'])
        self.assertEqual(event['pull_request']['branches'], ['main'])
        self.assertIn('concurrency', read_yaml('.github/workflows/backend-ci.yml'))
        self.assertFalse((ROOT / '.github/workflows/cla.yml').exists())

    def test_release_only_runs_on_own_tags_or_manual(self):
        self.assertEqual(events('release.yml')['push']['tags'], ['v*-N.*'])
        self.assertEqual(set(events('release.yml')), {'push', 'workflow_dispatch'})

    def test_formal_releases_are_not_github_prereleases(self):
        for name in ['.goreleaser.yaml', '.goreleaser.simple.yaml']:
            with self.subTest(name=name):
                self.assertIs(read_yaml(name)['release']['prerelease'], False)
