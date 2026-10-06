<?php
// Test-lab only: load the compose file of a git compose application (what
// "Reload Compose File" does in the UI) and give its web service a domain.
//   docker exec -e U=<app uuid> -e DOMAIN=http://web.e2e.test:8080 coolify php e2e_compose.php
chdir('/var/www/html');
require '/var/www/html/vendor/autoload.php';
$app = require '/var/www/html/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

$a = App\Models\Application::where('uuid', getenv('U'))->firstOrFail();
$a->loadComposeFile(true);
$a->refresh();
$a->docker_compose_domains = json_encode(['web' => ['domain' => getenv('DOMAIN')]]);
$a->save();
echo "OK\n";
