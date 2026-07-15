<?php

declare(strict_types=1);

namespace App\Reporting;

use App\Entity\Convoy;

final class CheckpointDigest
{
    /** @return list<string> */
    public function labels(Convoy $convoy): array
    {
        $labels = [];

        foreach ($convoy->checkpoints() as $checkpoint) {
            $labels[] = $checkpoint->label();
        }

        return $labels;
    }
}
