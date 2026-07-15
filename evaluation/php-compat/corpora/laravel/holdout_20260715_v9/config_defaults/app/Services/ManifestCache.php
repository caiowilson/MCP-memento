<?php

declare(strict_types=1);

namespace App\Services;

use Carbon\CarbonInterval;

final class ManifestCache
{
    public function expiration(): CarbonInterval
    {
        return CarbonInterval::minutes(
            (int) config('manifest.cache.fallback_lifetime_minutes', 19),
        );
    }
}
