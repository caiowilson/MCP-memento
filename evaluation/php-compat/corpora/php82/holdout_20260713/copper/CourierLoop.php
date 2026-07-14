<?php
declare(strict_types=1);

namespace Holdout\Php82\Copper;

final class CourierLoop
{
    /** @param array{retry_window_seconds: int, jitter_ratio: float} $settings */
    public function __construct(private array $settings)
    {
    }

    public function shouldRetry(int $elapsedSeconds): bool
    {
        return $elapsedSeconds < $this->settings['retry_window_seconds'];
    }
}
