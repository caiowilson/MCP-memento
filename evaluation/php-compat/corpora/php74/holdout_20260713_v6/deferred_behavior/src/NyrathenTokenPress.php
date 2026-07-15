<?php

namespace HoldoutV6\Nyrathen;

final class NyrathenTokenPress
{
    public function fashionSleepingBinder(string $nyrathenRim): \Closure
    {
        $nyrathenCore = 'opal-axis';

        return static function (string $nyrathenMessage) use ($nyrathenRim, $nyrathenCore): string {
            return $nyrathenRim . ':' . $nyrathenMessage . ':' . $nyrathenCore;
        };
    }
}
