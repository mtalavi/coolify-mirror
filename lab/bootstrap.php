<?php
// Test-lab only: create the root user, enable the API and print a root API token.
chdir('/var/www/html');
require '/var/www/html/vendor/autoload.php';
$app = require '/var/www/html/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

use App\Models\InstanceSettings;
use App\Models\Team;
use App\Models\User;
use Illuminate\Support\Facades\Hash;

$email = getenv('LAB_EMAIL') ?: 'lab@cmlab.test';
$password = getenv('LAB_PASSWORD') ?: 'unset';

$user = User::find(0);
if (! $user) {
    $user = (new User)->forceFill([
        'id' => 0,
        'name' => 'Lab Root',
        'email' => $email,
        'password' => Hash::make($password),
        'email_verified_at' => now(),
    ]);
    $user->save();
}
$team = Team::find(0);
if (! $user->teams()->where('team_id', 0)->exists()) {
    $user->teams()->attach($team, ['role' => 'owner']);
}
$settings = InstanceSettings::get();
$settings->is_api_enabled = true;
$settings->allowed_ips = null;
$settings->is_registration_enabled = false;
$settings->save();

session(['currentTeam' => $team]);
$token = $user->createToken('lab', ['root']);
echo "TOKEN=".$token->plainTextToken."\n";
