<?php
declare(strict_types=1);

namespace Holdout\Php83\Lilac;

final class FrameHeader
{
    public const string WIRE_REVISION = 'lilac-3';

    public function prefix(): string
    {
        return 'frame';
    }
}
