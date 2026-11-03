"""Isolated operator rehearsals. Run with python3 -I this_file.py.

Uses the deployed CLI, synthetic sessions and a disposable schema-23 lake.
Privilege dropping is substituted; no live host access is exercised.
"""
import contextlib
import datetime
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import pwd
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True


@contextlib.contextmanager
def fixture_db(path):
    db = sqlite3.connect(path)
    try:
        with db:
            yield db
    finally:
        db.close()


class OperatorRehearsal(unittest.TestCase):
    def setUp(self):
        root = Path('/home/sothr/.cache/agent-scratch/lampi')
        self.base = Path(tempfile.mkdtemp(prefix='vuoro-operator-test-', dir=root))
        self.data = self.base / 'lake'
        self.data.mkdir()
        self.binary = '/usr/local/bin/terva-lampi'
        server = subprocess.Popen([self.binary, 'serve', '--addr', '127.0.0.1:0', '--data', str(self.data)],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            for _ in range(200):
                if server.poll() is not None:
                    self.fail('isolated lake startup failed')
                try:
                    with fixture_db(self.data / 'catalog.db') as db:
                        ready = db.execute('PRAGMA user_version').fetchone()[0] == 23
                    if ready and (self.data / 'identity.json').exists():
                        break
                except sqlite3.Error:
                    pass
                time.sleep(.05)
            else:
                self.fail('isolated schema-23 startup timed out')
        finally:
            server.terminate()
            server.wait(timeout=30)
        self.uids = [f'synthetic-review-{n:04}' for n in range(1287)]
        now = datetime.datetime.now(datetime.timezone.utc).isoformat()
        with fixture_db(self.data / 'catalog.db') as db:
            for uid in self.uids + ['synthetic-development-control']:
                manifest = {'capture_protocol': 1, 'machine_id': 'synthetic-machine', 'harness': 'claude',
                            'native_session_id': uid, 'project': {'cwd': '/synthetic/' + uid,
                            'cwd_hash': uid, 'project_id': ''}, 'artifacts': []}
                db.execute('INSERT INTO sessions(session_uid,harness,native_session_id,head_sha256,manifest_json,ingested_at) VALUES(?,?,?,?,?,?)',
                           (uid, 'claude', uid, 'a' * 64, json.dumps(manifest), now))
                db.execute('INSERT INTO session_bays VALUES(?,?,?)', (uid, 'bay_default', now))
        proposal = {'batches': [{'request': {'session_uids': self.uids[n:n+500], 'add': ['code-reviews'],
                    'remove': ['default'], 'proposed_bays': ['code-reviews'], 'reason': 'Synthetic rehearsal'}}
                    for n in range(0, 1287, 500)], 'excluded': [{'session_uid': 'synthetic-development-control'}]}
        self.proposal = self.base / 'proposal.json'
        self.proposal.write_text(json.dumps(proposal))
        spec = importlib.util.spec_from_file_location('vuoro_rehearsal', Path(__file__).with_name('vuoro_approved_move.py'))
        self.runner = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.runner)
        self.runner.DATA = self.data
        self.runner.SCRATCH = self.base
        self.runner.SERVICE = pwd.getpwuid(os.getuid()).pw_name
        # Substitute only this synthetic fixture's approval, never the real file.
        self.runner.APPROVED_SHA256 = hashlib.sha256(self.proposal.read_bytes()).hexdigest()
        self.work = None

    def run_phase(self, phase):
        args = ['rehearsal', phase]
        if phase == 'preview':
            args += ['--proposal', str(self.proposal), '--sha256', self.runner.APPROVED_SHA256]
        else:
            args += ['--work', str(self.work)]
        output = io.StringIO()
        old_umask = os.umask(0o077)
        try:
            with patch.object(sys, 'argv', args), patch.dict(os.environ, {'SUDO_UID': str(os.getuid()), 'SUDO_GID': str(os.getgid())}), \
                    patch.object(os, 'geteuid', return_value=0), patch.object(os, 'initgroups'), contextlib.redirect_stdout(output):
                self.runner.main()
        finally:
            os.umask(old_umask)
            self.work = next(self.base.glob('vuoro-approved-*'), None)
        return json.loads(output.getvalue().splitlines()[-1])

    def moved(self):
        with fixture_db(self.data / 'catalog.db') as db:
            return db.execute("SELECT count(*) FROM session_bays WHERE bay_id != 'bay_default'").fetchone()[0]

    def test_complete_move_and_followup(self):
        self.run_phase('preview')
        self.assertTrue(self.run_phase('apply')['complete'])
        report = self.run_phase('verify')
        self.assertEqual(report['moved'], 1287)
        self.assertEqual(report['remaining'], 0)
        self.assertTrue(report['unselected_memberships_preserved'])
        self.assertTrue(report['no_new_grants'])

    def test_pinned_proposal_in_every_phase(self):
        original = self.proposal.read_bytes()
        self.proposal.write_bytes(original + b' ')
        with self.assertRaisesRegex(RuntimeError, 'checksum'):
            self.run_phase('preview')
        self.proposal.write_bytes(original)
        self.run_phase('preview')
        (self.work / 'proposal.json').write_bytes(original + b' ')
        for phase in ['apply', 'verify']:
            with self.assertRaisesRegex(RuntimeError, 'frozen proposal'):
                self.run_phase(phase)
        self.assertEqual(self.moved(), 0)

    def test_incomplete_preview_cannot_apply(self):
        self.run_phase('preview')
        (self.work / 'preview-complete.json').unlink()
        with self.assertRaises(FileNotFoundError):
            self.run_phase('apply')
        self.assertEqual(self.moved(), 0)

    def test_changed_backup_cannot_apply(self):
        self.run_phase('preview')
        with (self.work / 'catalog-before.db').open('ab') as f:
            f.write(b'changed')
        with self.assertRaisesRegex(RuntimeError, 'artifacts changed'):
            self.run_phase('apply')
        self.assertEqual(self.moved(), 0)

    def test_committed_receipt_recovered_after_output_loss(self):
        self.run_phase('preview')
        real_run = subprocess.run

        def interrupted(args, **kwargs):
            result = real_run(args, **kwargs)
            if 'apply-plan' in args and result.returncode == 0:
                raise RuntimeError('simulated interruption after committed apply')
            return result

        with patch.object(subprocess, 'run', side_effect=interrupted):
            with self.assertRaisesRegex(RuntimeError, 'simulated interruption'):
                self.run_phase('apply')
        self.assertEqual(self.moved(), 500)
        self.assertFalse((self.work / 'receipt-1.stdout').exists())
        report = self.run_phase('verify')
        self.assertEqual(report['moved'], 500)
        self.assertEqual(report['remaining'], 787)
        self.assertFalse(report['complete'])
        with self.assertRaisesRegex(RuntimeError, 'already attempted'):
            self.run_phase('apply')

    def test_concurrent_grant_stops_later_batches(self):
        self.run_phase('preview')
        real_run = subprocess.run

        def concurrent_grant(args, **kwargs):
            result = real_run(args, **kwargs)
            if 'plan' in args and 'apply-2-request.json' in args and result.returncode == 0:
                with fixture_db(self.data / 'catalog.db') as db:
                    bay = db.execute("SELECT id FROM bays WHERE name='code-reviews'").fetchone()[0]
                    db.execute('INSERT INTO bay_grants VALUES(?,?,?,?,?,?)',
                               ('group', 'synthetic-reader', bay, 'read', '2026-11-03T00:00:00Z', 'rehearsal'))
            return result

        with patch.object(subprocess, 'run', side_effect=concurrent_grant):
            with self.assertRaisesRegex(RuntimeError, 'grants/rules changed before batch apply'):
                self.run_phase('apply')
        self.assertEqual(self.moved(), 500)
        self.assertTrue((self.work / 'batch-1-receipt.json').exists())


if __name__ == '__main__':
    unittest.main()
