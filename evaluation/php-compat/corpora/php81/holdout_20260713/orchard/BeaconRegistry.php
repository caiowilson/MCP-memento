<?php
declare(strict_types=1);

namespace Holdout\Php81\Orchard;

use ReflectionMethod;

final class BeaconRegistry
{
    public function channelFor(string $class, string $method): ?string
    {
        $reflection = new ReflectionMethod($class, $method);
        $attributes = $reflection->getAttributes(BeaconHint::class);

        if ($attributes === []) {
            return null;
        }

        return $attributes[0]->newInstance()->channel;
    }
}
