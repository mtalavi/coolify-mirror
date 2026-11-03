<?php
// Lab-only fixtures: exercise new encrypted columns through the real Laravel
// models. Never print their values or a usable API token into the CI log.
chdir('/var/www/html');
require '/var/www/html/vendor/autoload.php';
$app = require '/var/www/html/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

use App\Models\Application;
use App\Models\User;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

$seed = getenv('ACTION') === 'seed';
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
        $token = User::findOrFail(0)->createToken('e2e-scoped-paused', ['read'])->accessToken;
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
echo "ok  locked environment variable preserved\n";
