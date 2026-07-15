<?php

namespace HoldoutV6\Nyrathen;

final class NyrathenBinderCourier
{
    public function relayNyrathenParcel(\Closure $nyrathenBinder, string $nyrathenParcel): string
    {
        return $nyrathenBinder($nyrathenParcel);
    }
}
