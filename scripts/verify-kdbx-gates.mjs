import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const uiPackage = 'blocowallet/internal/ui';
const uiGroups = {
  G1: ['TestKeePassUIPromptRendersOverDeleteView', 'TestKeePassUIPromptRendersOverRecoveryView', 'TestKeePassUIPromptDispatchesBlurQuitAndClock'],
  G2: ['TestKeePassUIPersonalSignResolvesBackupPassword', 'TestKeePassUICancelPropagatesToWorker'],
  G3: ['TestKeePassUICreateConfirmBackup', 'TestKeePassUIBlurSuspendsAndResumesCreation', 'TestKeePassUIRotateAndExport', 'TestKeePassUIPartialResultsPreserved'],
  G4: ['TestKeePassUISourcePreviewAndCommit', 'TestKeePassUISourcePwdSidecarPriority', 'TestKeePassUIImportMethods'],
  G5: ['TestKeePassUIBatchSources', 'TestKeePassUIMnemonicBatch', 'TestKeePassUIBatchSourceOptOutAndErrors'],
  G6: ['TestKeePassUIPendingRetries'],
  G7: ['TestKeePassUIConfigureAndBackupExisting'],
  G8: ['TestKeePassUIConfigureCancelThenRetry', 'TestKeePassUIDisabledTestRejected', 'TestKeePassUISettingsValidation', 'TestKeePassUISettingsSaveFailure', 'TestKeePassUISettingsAutocomplete'],
  G9: ['TestKeePassUINativeTransferResolvesBackupPassword', 'TestKeePassUIEIP712ResolvesBackupPassword', 'TestKeePassUIContractCallResolvesBackupPassword', 'TestKeePassUIRecoveryRequiresConfirmation', 'TestKeePassUISafeCredentials', 'TestKeePassUIDeletePreservesBackupByDefault', 'TestKeePassUIDeleteRemoveRequiresTypedConfirmation'],
  G10: ['TestKeePassUILayoutAndLocale', 'TestKeePassUINoSecretMessages', 'TestKeePassUIStaleResults'],
};
const allUITests = [...new Set(Object.values(uiGroups).flat())];

function judgeGoResult(result, required = [], packages = [], requireTests = true) {
  if (result.error || result.status !== 0 || result.signal) {
    throw new Error(`Go command failed (exit=${result.status}, signal=${result.signal ?? 'none'}); rerun the named command for diagnostics`);
  }
  const events = String(result.stdout ?? '').split(/\r?\n/).filter(line => line.trim()).map(line => JSON.parse(line));
  const runs = new Set();
  const passes = new Set();
  const packagePasses = new Set();
  const skips = new Set();
  for (const event of events) {
    if (!event || typeof event.Action !== 'string' || typeof event.Package !== 'string') throw new Error('Invalid Go test event');
    if (event.Action === 'fail') throw new Error(`Failed test/package: ${event.Test ?? event.Package}`);
    if (event.Test) {
      const key = `${event.Package}:${event.Test}`;
      if (event.Action === 'run') runs.add(key);
      if (event.Action === 'pass') passes.add(key);
      if (event.Action === 'skip') skips.add(key);
    } else if (event.Action === 'pass') {
      packagePasses.add(event.Package);
    }
  }
  if (!packagePasses.size) throw new Error('No passing Go test package was observed');
  if (requireTests && !runs.size) throw new Error('No tests ran');
  for (const pkg of packages) {
    if (!packagePasses.has(pkg)) throw new Error(`Package did not pass: ${pkg}`);
  }
  for (const key of required) {
    if (!runs.has(key) || !passes.has(key)) throw new Error(`Required test missing, skipped, or incomplete: ${key}`);
    for (const skipped of skips) {
      if (skipped === key || skipped.startsWith(`${key}/`)) throw new Error(`Required case skipped: ${skipped}`);
    }
  }
  return events;
}

function selfTest() {
  const event = (Action, Test) => JSON.stringify({ Action, Package: 'fixture/p', ...(Test ? { Test } : {}) });
  const successful = { status: 0, stdout: [event('run', 'TestRequired'), event('pass', 'TestRequired'), event('pass')].join('\n') };
  assert.doesNotThrow(() => judgeGoResult(successful, ['fixture/p:TestRequired'], ['fixture/p']));
  assert.throws(() => judgeGoResult({ status: 0, stdout: event('pass') }, ['fixture/p:TestRequired']), /No tests ran/);
  assert.throws(() => judgeGoResult(successful, ['fixture/p:TestAbsent']), /Required test missing/);
  assert.throws(() => judgeGoResult({ status: 0, stdout: [event('run', 'TestRequired'), event('skip', 'TestRequired'), event('pass')].join('\n') }, ['fixture/p:TestRequired']), /Required test missing/);
  assert.throws(() => judgeGoResult({ ...successful, stdout: `${successful.stdout}\n${event('skip', 'TestRequired/case')}` }, ['fixture/p:TestRequired']), /Required case skipped/);
  assert.throws(() => judgeGoResult({ ...successful, status: 1 }), /Go command failed/);
  assert.throws(() => judgeGoResult({ ...successful, stdout: `${successful.stdout}\n${event('fail', 'TestRequired')}` }), /Failed test/);
  assert.throws(() => judgeGoResult({ status: 0, stdout: 'ok' }), SyntaxError);
  assert.throws(() => judgeGoResult({ ...successful, signal: 'SIGTERM' }), /Go command failed/);
}

function command(program, args, env, cwd = root, timeout = 480000) {
  const result = spawnSync(program, args, { cwd, env, encoding: 'utf8', timeout, maxBuffer: 32 * 1024 * 1024, windowsHide: true });
  if (result.error || result.status !== 0 || result.signal) {
    throw new Error(`${program} ${args.join(' ')} failed (exit=${result.status}, signal=${result.signal ?? 'none'})`);
  }
  return result;
}

function environment(home) {
  const cached = JSON.parse(command('go', ['env', '-json', 'GOPATH', 'GOMODCACHE', 'GOCACHE'], process.env).stdout);
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.startsWith('BLOCO_WALLET_') || key.startsWith('BLOCOWALLET_')) delete env[key];
  }
  Object.assign(env, cached, {
    HOME: home,
    USERPROFILE: home,
    XDG_CONFIG_HOME: join(home, '.config'),
    XDG_CACHE_HOME: join(home, '.cache'),
    APPDATA: join(home, 'AppData', 'Roaming'),
    LOCALAPPDATA: join(home, 'AppData', 'Local'),
    BLOCO_WALLET_APP_APP_DIR: join(home, 'wallet-data'),
  });
  return env;
}

function goTests(pkg, names, env, { race = false, full = false, tags = '', short = false } = {}) {
  const args = ['test', '-json', '-count=1', '-timeout=360s'];
  if (race) args.push('-race');
  if (tags) args.push(`-tags=${tags}`);
  if (short) args.push('-short');
  if (!full) args.push('-run', `^(${names.join('|')})$`);
  args.push(pkg);
  const result = spawnSync('go', args, { cwd: root, env, encoding: 'utf8', timeout: 420000, maxBuffer: 32 * 1024 * 1024, windowsHide: true });
  const importPath = pkg === './third_party/gokeepasslib' ? 'github.com/tobischo/gokeepasslib/v3' : `blocowallet/${pkg.replace(/^\.\//, '')}`;
  judgeGoResult(result, names.map(name => `${importPath}:${name}`), pkg === './...' ? [] : [importPath]);
}

function checkPublication(env) {
  const expected = '257926a46375a14b21dde8a0b5f507fd0259ec5e';
  const url = 'git@github.com:italoag/gokeepasslib.git';
  const remote = command('git', ['ls-remote', url, 'refs/heads/fix/bounded-kdbx-decoding'], process.env).stdout.trim().split(/\s+/);
  assert.equal(remote[0], expected);
  assert.equal(remote[1], 'refs/heads/fix/bounded-kdbx-decoding');
  assert.equal(command('git', ['-C', 'third_party/gokeepasslib', 'rev-parse', 'HEAD'], process.env).stdout.trim(), expected);
  assert.equal(command('git', ['config', '--file', '.gitmodules', '--get', 'submodule.third_party/gokeepasslib.url'], process.env).stdout.trim(), url);
  const dependency = JSON.parse(command('go', ['list', '-m', '-json', 'github.com/tobischo/gokeepasslib/v3'], env).stdout);
  assert.equal(resolve(dependency.Replace.Dir), join(root, 'third_party', 'gokeepasslib'));
}

const gate = process.argv[2];
if (!gate || process.argv.length !== 3) {
  console.error('Usage: node scripts/verify-kdbx-gates.mjs G0..G20');
  process.exit(2);
}
let home;
try {
  if (gate === 'G0') {
    selfTest();
  } else {
    home = mkdtempSync(join(tmpdir(), 'bloco-kdbx-gates-'));
    const env = environment(home);
    if (uiGroups[gate]) {
      goTests('./internal/ui', uiGroups[gate], env);
    } else if (gate === 'G11') {
      goTests('./internal/ui', allUITests, env, { full: true });
    } else if (gate === 'G12') {
      goTests('./internal/ui', allUITests, env, { race: true });
    } else if (gate === 'G13') {
      goTests('./pkg/localization', [], env, { full: true });
      goTests('./cmd/blocowallet', [], env, { full: true });
      const output = join(home, process.platform === 'win32' ? 'bloco-wallet.exe' : 'bloco-wallet');
      command('go', ['build', '-tags=netgo,nocgo', '-o', output, './cmd/blocowallet'], { ...env, CGO_ENABLED: '0' });
      const version = command(output, ['--version'], env).stdout;
      assert.match(version, /^bloco-wallet-manager version /m);
    } else if (gate === 'G14') {
      goTests('./internal/keepass', ['TestStoreCreateInspectOpen', 'TestStoreAtomicBatch', 'TestStoreCommitRotatesCrypto', 'TestStoreFakeClockExpiry', 'TestStoreCreateDirSyncWarning'], env, { full: true });
      goTests('./internal/wallet', ['TestCredentialBackupCreateConfirmSync', 'TestCredentialBackupMnemonicExportFields', 'TestCredentialBackupForeignPendingDeletePreserved', 'TestCredentialBackupSupersededIntentNotAcked', 'TestCredentialBackupEncryptedArtifacts', 'TestCredentialBackupStoreCommitFailureRetry', 'TestCredentialBackupWithAccountPasswordStaleEntry', 'TestCredentialBackupConcurrentLifecycle'], env);
      goTests('./internal/storage', ['TestCredentialBackupDeleteBlockedLeavesNoIntent', 'TestCredentialBackupDeleteLedgerFailureRollsBack', 'TestCredentialBackupDeletePhysicalPreservesPendingDelete'], env);
      goTests('./pkg/config', ['TestKeePassConfigManagerRoundTrip', 'TestKeePassConfigLoadConfigRoundTrip', 'TestKeePassConfigValidation'], env);
    } else if (gate === 'G15') {
      const cli = process.env.KEEPASSXC_CLI || 'keepassxc-cli';
      command(cli, ['--version'], env, root, 20000);
      goTests('./internal/keepass', ['TestKeePassStoreInterop'], { ...env, KEEPASSXC_CLI: cli });
    } else if (gate === 'G16') {
      goTests('./...', [], env, { full: true, short: true });
    } else if (gate === 'G17') {
      checkPublication(env);
    } else if (gate === 'G18') {
      for (const goos of ['windows', 'linux']) {
        command('go', ['test', '-c', '-o', join(home, `${goos}-keepass.test`), './internal/keepass'], { ...env, CGO_ENABLED: '0', GOOS: goos, GOARCH: 'amd64' });
      }
    } else if (gate === 'G20') {
      goTests('./pkg/config', ['TestKeePassConfigLoadConfigExpandsHome'], env);
      goTests('./internal/wallet', ['TestTestsNeverWriteOutsideSandbox', 'TestCredentialBackupFreshConfirmationRequiresQueuedAccount', 'TestCredentialBackupCommittedWarningsRemainRecoverable'], env, { race: true });
      goTests('./internal/ui', ['TestKeePassUIExpiredCreationReauthenticates', 'TestKeePassUIBackupRefreshesAccountStatus', 'TestKeePassUIDurabilityWarningLocalized'], env, { race: true });
    } else if (gate === 'G19') {
      command('go', ['vet', './internal/keepass', './internal/wallet', './internal/storage', './internal/ui', './pkg/config'], env);
      command('golangci-lint', ['run', '--timeout=5m'], env);
      command('go', ['run', 'github.com/rhysd/actionlint/cmd/actionlint@v1.7.12'], env);
      command('git', ['diff', '--check'], process.env);
      command('git', ['diff', '--cached', '--check'], process.env);
    } else {
      throw new Error(`Unknown gate: ${gate}`);
    }
  }
  console.log(`KDBX_GATE_OK ${gate}`);
} catch (error) {
  console.error(`KDBX_GATE_FAILED ${gate}: ${String(error.message).replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ')}`);
  process.exitCode = 1;
} finally {
  if (home) rmSync(home, { recursive: true, force: true });
}
