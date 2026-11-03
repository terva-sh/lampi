#!/usr/bin/env python3
"""Constrained host runner for the owner-approved exact Vuoro move.

Run preview first. Inspect its complete results before invoking apply.
All catalog mutations go through the deployed CLI as its service account.
"""
import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import pwd
import sqlite3
import subprocess
import tempfile
import time

BINARY = '/usr/local/bin/terva-lampi'
DATA = Path('/var/lib/terva-lampi')
SCRATCH = Path('/home/sothr/.cache/agent-scratch/lampi')
SERVICE = 'terva-lampi'
APPROVED_SHA256 = '1c93128d1c84225ecf59bc740ee714e72f9c4a2543b62fc2be0d5ddb6eae6b4f'


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def main():
    p = argparse.ArgumentParser()
    p.add_argument('action', choices=['preview', 'apply', 'verify'])
    p.add_argument('--proposal', type=Path)
    p.add_argument('--sha256')
    p.add_argument('--work', type=Path)
    a = p.parse_args()
    if os.geteuid() != 0:
        raise RuntimeError('host runner requires sudo; no live action performed')
    os.umask(0o077)
    service = pwd.getpwnam(SERVICE)
    caller_uid = int(os.environ['SUDO_UID'])
    caller_gid = int(os.environ['SUDO_GID'])
    verify_tag = str(time.time_ns())

    def write(path, value, raw=False):
        with path.open('x') as f:
            f.write(value if raw else json.dumps(value, indent=2) + '\n')
            f.flush()
            os.fsync(f.fileno())
        os.chmod(path, 0o600)
        cli_input = path.name.endswith('-request.json') or path.name.endswith('-plan.json')
        os.chown(path, service.pw_uid if cli_input else caller_uid,
                 service.pw_gid if cli_input else caller_gid)

    if a.action == 'preview':
        raw = a.proposal.read_bytes()
        if a.sha256 != APPROVED_SHA256 or hashlib.sha256(raw).hexdigest() != APPROVED_SHA256:
            raise RuntimeError('proposal checksum changed')
        proposal = json.loads(raw)
        work = Path(tempfile.mkdtemp(prefix='vuoro-approved-', dir=SCRATCH))
        os.chown(work, service.pw_uid, caller_gid)
        os.chmod(work, 0o770)
        write(work / 'proposal.json', raw.decode(), raw=True)
    else:
        work = a.work.resolve(strict=True)
        if work.parent != SCRATCH or not work.name.startswith('vuoro-approved-'):
            raise RuntimeError('unexpected operator work directory')
        raw = (work / 'proposal.json').read_bytes()
        if hashlib.sha256(raw).hexdigest() != APPROVED_SHA256:
            raise RuntimeError('work proposal differs from owner-approved frozen proposal')
        proposal = json.loads(raw)

    batches = proposal['batches']
    uids = [u for b in batches for u in b['request']['session_uids']]
    if len(uids) != 1287 or len(set(uids)) != 1287:
        raise RuntimeError('approved UID count/uniqueness differs')
    if set(uids) & {x['session_uid'] for x in proposal['excluded']}:
        raise RuntimeError('excluded control appears in approved set')
    for b in batches:
        r = b['request']
        if r['add'] != ['code-reviews'] or r['remove'] != ['default'] or r['proposed_bays'] != ['code-reviews'] or len(r['session_uids']) > 500:
            raise RuntimeError('request is outside approved membership edits')

    def drop():
        # Home is private. Enter the new service-owned directory before
        # dropping privileges; CLI input paths are relative to this cwd.
        os.chdir(work)
        os.initgroups(SERVICE, service.pw_gid)
        os.setgid(service.pw_gid)
        os.setuid(service.pw_uid)

    def cli(args, label, structured=True):
        allowed = {'list', 'grants', 'rules', 'plan', 'apply-plan', 'verify-plan'}
        if not args or args[0] not in allowed:
            raise RuntimeError('unapproved CLI operation')
        result = subprocess.run([BINARY, 'serve', 'bays', *args, '--data', str(DATA)],
                                stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, preexec_fn=drop, timeout=120)
        write(work / (label + '.stdout'), result.stdout, raw=True)
        if result.stderr:
            write(work / (label + '.stderr'), result.stderr, raw=True)
        if result.returncode:
            raise RuntimeError(f'{label} failed; inspect its private output before retrying')
        return json.loads(result.stdout) if structured else result.stdout

    def state():
        db = sqlite3.connect('file:' + str(DATA / 'catalog.db') + '?mode=ro', uri=True, timeout=30)
        try:
            rows = lambda q: [list(x) for x in db.execute(q)]
            out = {name: rows('SELECT * FROM ' + name + ' ORDER BY 1,2')
                   for name in ['bay_grants', 'bay_rules', 'bays', 'bay_aliases']}
            out['memberships'] = rows('SELECT session_uid,bay_id,added_at FROM session_bays ORDER BY session_uid,bay_id')
            wanted = set(uids)
            out['selected_manifest_bays'] = {u: json.loads(m).get('bays', []) for u, m in db.execute('SELECT session_uid,manifest_json FROM sessions') if u in wanted}
            out['selected_ingested_at'] = {u: t for u, t in db.execute('SELECT session_uid,ingested_at FROM sessions') if u in wanted}
            out['sessions'] = db.execute('SELECT count(*) FROM sessions').fetchone()[0]
            out['schema_version'] = db.execute('PRAGMA user_version').fetchone()[0]
            return out
        finally:
            db.close()

    def check_plan(plan, batch, proposed):
        ids = set(batch['request']['session_uids'])
        if not plan['complete_memberships'] or plan['rejected'] or plan['noop'] or plan['changed'] != len(ids):
            raise RuntimeError('complete administrator preview has rejection/no-op or unexpected counts')
        if {i['session_uid'] for i in plan['items']} != ids:
            raise RuntimeError('preview UID union differs from approved batch')
        if len(plan['destinations']) != 1 or plan['destinations'][0]['name'] != 'code-reviews' or plan['destinations'][0]['proposed'] != proposed:
            raise RuntimeError('unexpected destination')
        dest = plan['destinations'][0]['id']
        for item in plan['items']:
            before = {b['id'] for b in item['before']}
            after = {b['id'] for b in item['after']}
            if item['status'] != 'change' or 'bay_default' not in before or after != (before - {'bay_default'}) | {dest}:
                raise RuntimeError('preview would change unapproved membership')
            if item['add'] != [dest] or item['remove'] != ['bay_default']:
                raise RuntimeError('unexpected effective membership delta')
        return {'sessions': len(ids), 'plan_id': plan['plan_id'], 'complete_memberships': True,
                'changed': plan['changed'], 'rejected': plan['rejected'],
                'other_memberships': sum(len(i['before']) - 1 for i in plan['items'])}

    def preview_batch(batch, proposed, label):
        request = dict(batch['request'])
        if not proposed:
            request.pop('proposed_bays', None)
        write(work / (label + '-request.json'), request)
        plan = cli(['plan', '--request', label + '-request.json'], label)
        summary = check_plan(plan, batch, proposed)
        write(work / (label + '-plan.json'), plan)
        return plan, summary

    def preview_artifacts():
        return ['proposal.json', 'before.json', 'catalog-before.db', 'preview-summary.json'] + [
            f'preview-{n}{suffix}' for n in range(1, len(batches) + 1)
            for suffix in ['-request.json', '-plan.json', '.stdout']
        ] + [f'before-{command}.stdout' for command in ['list', 'grants', 'rules']]

    def artifact_digests():
        return {name: hashlib.sha256((work / name).read_bytes()).hexdigest()
                for name in preview_artifacts()}

    def require_preview():
        completion = json.loads((work / 'preview-complete.json').read_text())
        if completion != {'proposal_sha256': APPROVED_SHA256, 'artifacts': artifact_digests()}:
            raise RuntimeError('completed preview artifacts changed; regenerate and inspect')
        for n, batch in enumerate(batches, 1):
            check_plan(json.loads((work / f'preview-{n}-plan.json').read_text()), batch, True)
        backup = sqlite3.connect('file:' + str(work / 'catalog-before.db') + '?mode=ro', uri=True)
        try:
            if backup.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
                raise RuntimeError('completed preview backup is invalid')
        finally:
            backup.close()

    if a.action == 'preview':
        before = state()
        if before['schema_version'] != 23:
            raise RuntimeError('expected deployed catalog schema 23; stop before any apply')
        if any(b[1] == 'code-reviews' for b in before['bays']) or any(b[0] == 'code-reviews' for b in before['bay_aliases']):
            raise RuntimeError('proposed name already exists; inspect before any apply')
        write(work / 'before.json', before)
        for command in ['list', 'grants', 'rules']:
            cli([command], 'before-' + command, structured=False)
        summaries = []
        for n, batch in enumerate(batches, 1):
            _, summary = preview_batch(batch, True, f'preview-{n}')
            summaries.append(summary)
        # A consistent online catalog backup; no artifacts, identity or
        # capture configuration are changed by the membership operation.
        source = sqlite3.connect('file:' + str(DATA / 'catalog.db') + '?mode=ro', uri=True)
        backup_path = work / 'catalog-before.db'
        target = sqlite3.connect(backup_path)
        try:
            source.backup(target)
            if target.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
                raise RuntimeError('catalog backup check failed')
        finally:
            target.close()
            source.close()
        os.chmod(backup_path, 0o600)
        os.chown(backup_path, caller_uid, caller_gid)
        report = {'work': str(work), 'phase': 'preview', 'sessions': before['sessions'],
                  'bays': len(before['bays']), 'grants': len(before['bay_grants']),
                  'rules': len(before['bay_rules']), 'requested_bay_sets': dict(collections.Counter(json.dumps(v, sort_keys=True) for v in before['selected_manifest_bays'].values())),
                  'batches': summaries, 'backup_quick_check': 'ok',
                  'grants_sha256': digest(before['bay_grants']), 'rules_sha256': digest(before['bay_rules'])}
        write(work / 'preview-summary.json', report)
        # Written last: failed/incomplete previews cannot authorize apply.
        write(work / 'preview-complete.json', {'proposal_sha256': APPROVED_SHA256,
                                             'artifacts': artifact_digests()})
        print(json.dumps(report), flush=True)
        return

    require_preview()
    before = json.loads((work / 'before.json').read_text())
    if a.action == 'apply':
        # A partial apply is inspected with verify, never replayed blindly.
        if any(work.glob('apply-*-plan.json')):
            raise RuntimeError('apply already attempted; run verify for partial progress before recovery')
        current = state()
        if current['bay_grants'] != before['bay_grants'] or current['bay_rules'] != before['bay_rules'] or current['bays'] != before['bays'] or current['bay_aliases'] != before['bay_aliases']:
            raise RuntimeError('policy changed since reviewed preview; regenerate and inspect')
        receipts = []
        created_target_id = None
        for n, batch in enumerate(batches, 1):
            # Creation in batch one changes policy state. Later batches
            # freshly resolve the now-existing destination and are reviewed
            # against the same approved exact membership delta.
            plan, summary = preview_batch(batch, n == 1, f'apply-{n}')
            policy = state()
            if policy['bay_grants'] != before['bay_grants'] or policy['bay_rules'] != before['bay_rules']:
                raise RuntimeError('grants/rules changed before batch apply; stop and inspect')
            if created_target_id is not None and plan['destinations'][0]['id'] != created_target_id:
                raise RuntimeError('destination identity changed between batches')
            args = ['apply-plan', f'apply-{n}-plan.json', '--yes']
            if n == 1:
                args.append('--create-proposed')
            receipt = cli(args, f'receipt-{n}')
            write(work / f'batch-{n}-receipt.json', receipt)
            if not receipt['complete'] or len(receipt['items']) != len(batch['request']['session_uids']):
                raise RuntimeError('unexpected receipt; inspect before continuing')
            if {i['session_uid'] for i in receipt['items']} != set(batch['request']['session_uids']):
                raise RuntimeError('receipt UID set differs from approved batch')
            if n == 1:
                if len(receipt['created_bays']) != 1 or receipt['created_bays'][0]['name'] != 'code-reviews':
                    raise RuntimeError('unexpected bay creation receipt')
                created_target_id = receipt['created_bays'][0]['id']
            elif receipt['created_bays']:
                raise RuntimeError('later batch unexpectedly created a bay')
            receipts.append(receipt['plan_id'])
            verification = cli(['verify-plan', receipt['plan_id']], f'verify-{n}')
            if not verification['matches_receipt']:
                raise RuntimeError('receipt verification differs; stop before next batch')
            print(json.dumps({'batch': n, 'applied': len(receipt['items']), 'plan_id': receipt['plan_id'], 'matches_receipt': True}), flush=True)
        write(work / 'receipt-ids.json', receipts)
    else:
        # The CLI commits its receipt in the same catalog transaction as the
        # membership edit. Recover it even if this process died before saving
        # stdout or verifying a batch. Plans are durable before apply starts.
        receipts = []
        completed_uids = set()
        db = sqlite3.connect('file:' + str(DATA / 'catalog.db') + '?mode=ro', uri=True)
        try:
            for n, batch in enumerate(batches, 1):
                path = work / f'apply-{n}-plan.json'
                if not path.exists():
                    continue
                plan = json.loads(path.read_text())
                check_plan(plan, batch, n == 1)
                row = db.execute('SELECT result_json FROM bay_migrations WHERE id=?', (plan['plan_id'],)).fetchone()
                if row is None:
                    continue
                receipt = json.loads(row[0])
                if not receipt['complete'] or {i['session_uid'] for i in receipt['items']} != set(batch['request']['session_uids']):
                    raise RuntimeError('stored receipt differs from approved batch')
                write(work / f'followup-{verify_tag}-receipt-{n}.json', receipt)
                receipts.append(receipt['plan_id'])
                completed_uids.update(batch['request']['session_uids'])
        finally:
            db.close()
        for n, plan_id in enumerate(receipts, 1):
            verification = cli(['verify-plan', plan_id], f'followup-{verify_tag}-verify-{n}')
            if not verification['matches_receipt']:
                raise RuntimeError('later receipt verification differs')

    after = state()
    write(work / ('after.json' if a.action == 'apply' else f'followup-{verify_tag}-state.json'), after)
    if after['bay_grants'] != before['bay_grants'] or after['bay_rules'] != before['bay_rules']:
        raise RuntimeError('grant/rule inventory changed; inspect immediately')
    ids = set(uids) if a.action == 'apply' else completed_uids
    original_other = {(u, b, t) for u, b, t in before['memberships'] if u not in ids}
    after_other = {(u, b, t) for u, b, t in after['memberships'] if u not in ids}
    # New sessions may arrive; pre-existing unselected memberships must remain.
    before_uids = {u for u, _, _ in before['memberships']}
    if original_other != {x for x in after_other if x[0] in before_uids}:
        raise RuntimeError('unselected membership changed; inspect immediately')
    target_ids = {b[0] for b in after['bays'] if b[1] == 'code-reviews'}
    if ids and len(target_ids) != 1:
        raise RuntimeError('target bay missing or ambiguous')
    target_id = next(iter(target_ids), None)
    member = {(u, b) for u, b, _ in after['memberships']}
    if any((u, target_id) not in member or (u, 'bay_default') in member for u in ids):
        raise RuntimeError('approved split does not match actual membership')
    report = {'work': str(work), 'phase': a.action, 'moved': len(ids), 'remaining': len(uids) - len(ids),
              'complete': len(ids) == len(uids), 'target_id': target_id,
              'no_new_grants': True, 'rules_unchanged': True, 'unselected_memberships_preserved': True,
              # ingested_at advances for head changes, not every manifest
              # request. This cannot confirm or rule out unchanged reposts.
              'all_receipts_match': True, 'selected_ingested_at_changes_since_preview': sum(after['selected_ingested_at'].get(u) != before['selected_ingested_at'].get(u) for u in ids),
              'receipt_ids': receipts}
    write(work / ('apply-summary.json' if a.action == 'apply' else f'verify-{verify_tag}-summary.json'), report)
    print(json.dumps(report), flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # No CLI output, environment, auth files or password are printed.
        print(json.dumps({'error': str(error)}), flush=True)
        raise SystemExit(1)
