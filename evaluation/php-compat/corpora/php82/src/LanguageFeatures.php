<?php

declare(strict_types=1);

namespace Fixture\PHP82;

use Countable;
use IteratorAggregate;

trait HasKind
{
    public const KIND = 'packet';
}

readonly class Packet
{
    use HasKind;

    public function __construct(
        public string $id,
        public (Countable&IteratorAggregate)|null $items = null,
    ) {
    }

    public function ready(): true
    {
        return true;
    }

    public function missing(): null
    {
        return null;
    }
}
