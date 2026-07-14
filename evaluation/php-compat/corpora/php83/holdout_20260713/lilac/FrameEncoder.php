<?php
declare(strict_types=1);

namespace Holdout\Php83\Lilac;

final class FrameEncoder
{
    public function encode(string $payload): string
    {
        return FrameHeader::WIRE_REVISION . ':' . $payload;
    }
}
