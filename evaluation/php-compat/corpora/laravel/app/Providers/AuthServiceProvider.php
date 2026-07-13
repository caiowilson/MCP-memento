<?php

declare(strict_types=1);

namespace App\Providers;

use App\Models\Report;
use App\Policies\ReportPolicy;
use Illuminate\Support\Facades\Gate;

final class AuthServiceProvider
{
    public function boot(): void
    {
        Gate::policy(Report::class, ReportPolicy::class);
    }
}
