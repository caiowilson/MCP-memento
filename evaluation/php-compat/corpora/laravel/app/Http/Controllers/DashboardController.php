<?php

declare(strict_types=1);

namespace App\Http\Controllers;

use App\Services\ReportService;
use Illuminate\View\View;

final class DashboardController
{
    public function show(ReportService $reports): View
    {
        $stripeKey = config('services.stripe.key');

        return view('dashboard.index', [
            'reports' => $reports->recent(),
            'stripeKey' => $stripeKey,
        ]);
    }
}
