<?php

declare(strict_types=1);

namespace Fixture\PHP82;

final class PacketQueue
{
    public function items(iterable $packets): iterable
    {
        return $packets;
    }
}
