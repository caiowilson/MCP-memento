<?php
declare(strict_types=1);

namespace Holdout\Php81\Orchard;

use Attribute;

#[Attribute(Attribute::TARGET_METHOD)]
final class BeaconHint
{
    public function __construct(public string $channel)
    {
    }
}

final class OrchardEndpoint
{
    #[BeaconHint('orchard.read')]
    public function inspect(string $bin): array
    {
        return ['bin' => $bin];
    }
}
