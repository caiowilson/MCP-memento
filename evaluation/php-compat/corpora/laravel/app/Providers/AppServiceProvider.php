<?php

declare(strict_types=1);

namespace App\Providers;

use App\Contracts\ReportRepository;
use App\Repositories\DatabaseReportRepository;
use Illuminate\Support\ServiceProvider;

final class AppServiceProvider extends ServiceProvider
{
    public function register(): void
    {
        $this->app->bind(ReportRepository::class, DatabaseReportRepository::class);
    }
}
