<?php
// Lab-only fixtures: exercise new encrypted columns through the real Laravel
// models. Never print their values or a usable API token into the CI log.
chdir('/var/www/html');
require '/var/www/html/vendor/autoload.php';
$app = require '/var/www/html/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

use App\Livewire\Forms\ApiTokenForm;
use App\Models\Application;
use App\Models\PersonalAccessToken;
use App\Models\User;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

$seed = getenv('ACTION') === 'seed';
$hasScopeParserFix = version_compare(ltrim((string) getenv('LAB_COOLIFY_VERSION'), 'v'), '4.7.5', '>=');
$application = Application::where('uuid', getenv('APP_UUID'))->firstOrFail();
$variable = $application->environment_variables()->where('key', 'SECRET_TOKEN')->firstOrFail();
if ($seed) {
    $variable->is_shown_once = true;
    $variable->save();
} elseif (! $variable->is_shown_once || $variable->value !== 'e2e-secret-token-42') {
    throw new RuntimeException('locked environment variable did not survive');
}

// Older releases have no HTTPS credentials; keep running the same lab there.
if (Schema::hasColumn('applications', 'git_https_password')) {
    if ($seed) {
        $application->git_https_username = 'e2e-git-user';
        $application->git_https_password = 'e2e-git-password-46';
        $application->save();
    } elseif ($application->git_https_username !== 'e2e-git-user' || $application->git_https_password !== 'e2e-git-password-46') {
        throw new RuntimeException('HTTPS Git credentials did not decrypt with the target APP_KEY');
    }
    if (DB::table('applications')->where('id', $application->id)->value('git_https_password') === 'e2e-git-password-46') {
        throw new RuntimeException('HTTPS Git password was stored without encryption');
    }
    echo "ok  HTTPS Git credentials encrypted at rest and readable by Laravel\n";
}

// A full move carries users and tokens, including 4.6 scopes and paused state.
// A selective move must not grant the old server's users access to the target.
if (Schema::hasColumn('personal_access_tokens', 'scopes')) {
    $scopes = [['scope' => 'resource', 'uuid' => $application->uuid]];
    if ($seed) {
        $token = User::findOrFail(0)->createToken('e2e-scoped-paused', ['read'], null, 0)->accessToken;
        $token->forceFill(['scopes' => $scopes, 'paused_at' => '2026-11-01 00:00:00'])->save();
    } else {
        $token = DB::table('personal_access_tokens')->where('name', 'e2e-scoped-paused')->first();
        if (getenv('MODE') === 'selective') {
            if ($token) {
                throw new RuntimeException('selective restore copied a source API token');
            }
            echo "ok  selective restore did not copy source API access\n";
        } else {
            if (! $token || json_decode($token->scopes, true) !== $scopes || json_decode($token->abilities, true) !== ['read'] || ! $token->paused_at) {
                throw new RuntimeException('full restore lost token scopes, abilities or paused state');
            }
            echo "ok  full restore preserved scoped API token permissions and paused state\n";
        }
    }
}

// 4.7.5 can open the edit form when an older token contains unreadable
// scope entries. Keep one valid grant beside an invalid entry to check both.
if ($hasScopeParserFix && Schema::hasColumn('personal_access_tokens', 'scopes')) {
    $name = 'e2e-legacy-scoped-paused';
    $scopes = [['scope' => 'resource', 'uuid' => $application->uuid], 'legacy-entry'];
    if ($seed) {
        $token = User::findOrFail(0)->createToken($name, ['read'], null, 0)->accessToken;
        $token->forceFill(['scopes' => $scopes, 'paused_at' => '2026-11-01 00:00:00'])->save();
    } elseif (getenv('MODE') === 'selective') {
        if (PersonalAccessToken::where('name', $name)->exists()) {
            throw new RuntimeException('selective restore copied the legacy source token');
        }
        echo "ok  selective restore excluded the legacy scoped token\n";
    } else {
        $token = PersonalAccessToken::where('name', $name)->firstOrFail();
        $values = ApiTokenForm::valuesOf($token);
        if ($token->scopes !== $scopes || ! $token->isPaused() ||
            $values['permissions'] !== ['read'] || $values['scopeMode'] !== 'selected' ||
            $values['selectedScopes'] !== ['resource:'.$application->uuid]) {
            throw new RuntimeException('legacy token state or valid grant did not survive full restore');
        }
        echo "ok  restored legacy token opens in the Coolify form with its valid grant and read permission\n";
    }
}
echo "ok  locked environment variable preserved\n";

// 4.7 stores SFTP destinations in s3_storages. The key is deliberately used
// only by that destination, so a missing transitive dependency cannot hide.
if (Schema::hasColumn('s3_storages', 'private_key_id')) {
    $multiline = "first line\nsecond line\n";
    $endpoint = 'sftp://[2001:db8::25]:2222/team%20backups';
    $hasDatabaseDestinations = Schema::hasTable('scheduled_database_backup_s3_storage');
    $hasVolumeDestinations = Schema::hasTable('scheduled_volume_backup_s3_storage');
    $database = App\Models\StandalonePostgresql::where('name', 'e2e-db')->firstOrFail();
    if ($hasVolumeDestinations) {
        $web = Application::where('uuid', getenv('WEB_UUID'))->firstOrFail();
        $volume = $web->persistentStorages()->firstOrFail();
    }
    if ($seed) {
        $key = App\Models\PrivateKey::create([
            'name' => 'e2e-storage-key', 'team_id' => 0, 'is_git_related' => false,
            'private_key' => phpseclib3\Crypt\EC::createKey('Ed25519')->toString('OpenSSH'),
        ]);
        $storage = App\Models\S3Storage::create([
            'name' => 'e2e-sftp-settings', 'team_id' => 0, 'type' => 'sftp',
            'endpoint' => $endpoint, 'bucket' => null,
            'key' => 'e2e-storage-user', 'secret' => 'e2e-storage-password',
            'private_key_id' => $key->id,
        ]);
        $storage->sftp_host_key = trim($key->public_key);
        $storage->save();
        // Make SFTP a secondary destination so the legacy column cannot
        // accidentally carry the only storage that needs the private key.
        if ($hasDatabaseDestinations || $hasVolumeDestinations) {
            $primary = App\Models\S3Storage::create([
                'name' => 'e2e-primary-storage', 'team_id' => 0, 'type' => 's3',
                'endpoint' => 'https://s3.e2e.test', 'bucket' => 'e2e-backups',
                'key' => 'lab-s3-key', 'secret' => 'lab-s3-secret',
            ]);
        }
        $backup = App\Models\ScheduledDatabaseBackup::create([
            'team_id' => 0, 'enabled' => false, 'save_s3' => true, 'frequency' => '0 0 1 1 *',
            'database_type' => $database->getMorphClass(), 'database_id' => $database->id,
            's3_storage_id' => $hasDatabaseDestinations ? $primary->id : $storage->id,
        ]);
        if ($hasDatabaseDestinations) {
            $backup->syncS3Storages([$primary->id, $storage->id]);
        }
        if ($hasVolumeDestinations) {
            $volumeBackup = App\Models\ScheduledVolumeBackup::create([
                'team_id' => 0, 'enabled' => false, 'save_s3' => true, 'frequency' => '0 0 1 1 *',
                'backupable_type' => $volume->getMorphClass(), 'backupable_id' => $volume->id,
                's3_storage_id' => $primary->id,
            ]);
            $volumeBackup->syncS3Storages([$primary->id, $storage->id]);
        }
        $application->environment_variables()->create([
            'key' => 'E2E_MULTILINE', 'value' => $multiline, 'is_multiline' => true,
            'is_runtime' => false, 'is_buildtime' => false,
        ]);
        $application->git_ssh_known_hosts = 'git.e2e.test '.trim($key->public_key);
        $application->save();
    } else {
        $storage = App\Models\S3Storage::where('name', 'e2e-sftp-settings')->firstOrFail();
        $key = App\Models\PrivateKey::findOrFail($storage->private_key_id);
        if ($key->name !== 'e2e-storage-key' || $key->team_id !== $storage->team_id ||
            App\Models\PrivateKey::generateFingerprint($key->private_key) !== $key->fingerprint ||
            $storage->secret !== 'e2e-storage-password' || $storage->key !== 'e2e-storage-user' ||
            $storage->type->value !== 'sftp' || $storage->sftp_host_key !== trim($key->public_key)) {
            throw new RuntimeException('SFTP settings, host pin or private key not preserved');
        }
        if ($storage->endpoint !== $endpoint ||
            App\Models\S3Storage::splitServerEndpoint($storage->endpoint) !== ['host' => '2001:db8::25', 'port' => '2222', 'path' => 'team backups']) {
            throw new RuntimeException('SFTP IPv6 endpoint, port or directory did not survive');
        }
        $backup = App\Models\ScheduledDatabaseBackup::whereMorphedTo('database', $database)->where('frequency', '0 0 1 1 *')->firstOrFail();
        if ($backup->enabled || (! $hasDatabaseDestinations && $backup->s3_storage_id !== $storage->id)) {
            throw new RuntimeException('database backup schedule was not preserved');
        }
        $destinations = ['e2e-primary-storage', 'e2e-sftp-settings'];
        if ($hasDatabaseDestinations || $hasVolumeDestinations) {
            $primary = App\Models\S3Storage::where('name', 'e2e-primary-storage')->firstOrFail();
        }
        if ($hasDatabaseDestinations) {
            if ($backup->selectedS3Storages()->pluck('name')->all() !== $destinations || $backup->s3_storage_id !== $primary->id) {
                throw new RuntimeException('database backup primary or secondary destination lost');
            }
            echo "ok  both database backup destinations preserved in order\n";
        }
        if ($hasVolumeDestinations) {
            $volumeBackup = App\Models\ScheduledVolumeBackup::whereMorphedTo('backupable', $volume)->where('frequency', '0 0 1 1 *')->firstOrFail();
            if ($volumeBackup->enabled || $volumeBackup->selectedS3Storages()->pluck('name')->all() !== $destinations ||
                $volumeBackup->s3_storage_id !== $primary->id) {
                throw new RuntimeException('volume backup schedule or destination lost');
            }
            echo "ok  both volume backup destinations preserved in order\n";
        }
        if ($application->git_ssh_known_hosts !== 'git.e2e.test '.trim($key->public_key)) {
            throw new RuntimeException('Git SSH known hosts not preserved');
        }
        if ($application->environment_variables()->where('key', 'E2E_MULTILINE')->firstOrFail()->value !== $multiline) {
            throw new RuntimeException('multiline value lost bytes or trailing newline');
        }
        echo "ok  SFTP destination, SSH key dependency, host pins and disabled schedule preserved\n";
        echo "ok  SFTP IPv6 endpoint, custom port and encoded directory preserved\n";
        echo "ok  multiline environment value including trailing newline preserved\n";
    }
}

if (Schema::hasTable('image_scan_policies')) {
    $policyQuery = App\Models\ImageScanPolicy::where('scope_type', $application->getMorphClass())->where('scope_id', $application->id);
    $ignoreQuery = App\Models\ImageScanIgnore::where('resource_type', $application->getMorphClass())->where('resource_id', $application->id);
    if ($seed) {
        App\Models\ImageScanPolicy::create(['scope_type' => $application->getMorphClass(), 'scope_id' => $application->id,
            'gate_mode' => 'off', 'on_scan_error' => 'deploy', 'count_medium' => true]);
        App\Models\ImageScanIgnore::create(['team_id' => 0, 'resource_type' => $application->getMorphClass(),
            'resource_id' => $application->id, 'package' => 'e2e-fixture-package', 'reason' => 'migration fixture', 'created_by' => 0]);
    } else {
        $policy = $policyQuery->firstOrFail();
        $ignore = $ignoreQuery->where('package', 'e2e-fixture-package')->firstOrFail();
        if ($policy->gate_mode->value !== 'off' || $policy->on_scan_error->value !== 'deploy' || ! $policy->count_medium ||
            $ignore->reason !== 'migration fixture' || (getenv('MODE') === 'selective' && $ignore->created_by !== null)) {
            throw new RuntimeException('resource scan policy or ignore not preserved');
        }
        echo "ok  resource vulnerability policy and ignore preserved\n";
    }
}
