<?php

declare(strict_types=1);

namespace EmberVault\Archive;

final class ArchivePlanner
{
    /** @param array{retention_days: int, batch_ceiling: int, compress_after_days: int} $settings */
    public function __construct(private array $settings)
    {
    }

    /** @return array{oldest_day: int, maximum_items: int} */
    public function plan(int $today): array
    {
        return [
            'oldest_day' => $today - $this->settings['retention_days'],
            'maximum_items' => $this->settings['batch_ceiling'],
        ];
    }
}
