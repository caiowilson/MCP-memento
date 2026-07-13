<?php

declare(strict_types=1);

namespace App\Policies;

use App\Models\Report;
use App\Models\User;

final class ReportPolicy
{
    public function view(User $user, Report $report): bool
    {
        return true;
    }
}
