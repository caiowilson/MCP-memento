<?php

declare(strict_types=1);

namespace RetrievalHoldout\Scheduling;

use DateTimeImmutable;

final class NextCheck
{
    public function from(DateTimeImmutable $startingAt): DateTimeImmutable
    {
        $days = (int) config('polling.pause_days');

        return $startingAt->modify("+{$days} days");
    }
}
