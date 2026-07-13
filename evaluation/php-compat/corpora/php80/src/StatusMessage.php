<?php

declare(strict_types=1);

namespace Fixture\PHP80;

final class StatusMessage
{
    public function message(string $status): string
    {
        return $status;
    }
}
