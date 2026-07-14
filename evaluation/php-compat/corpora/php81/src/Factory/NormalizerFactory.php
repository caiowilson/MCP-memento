<?php

declare(strict_types=1);

namespace Fixture\PHP81\Factory;

use Fixture\PHP81\Support\Normalizer;

final class NormalizerFactory
{
    public function create(Normalizer $normalizer): callable
    {
        return $normalizer->normalize(...);
    }
}
