<?php

declare(strict_types=1);

namespace Fixture\Composer\App;

use Fixture\Composer\FallbackOnly;
use Fixture\Composer\Service;
use Fixture\Composer\Special\Handler;
use Odd\Location\MappedThing;
use Old\Domain\Report;

require_once __DIR__ . '/../bootstrap/helpers.php';

final class Consumer
{
    public function __construct(
        private Service $service,
        private Handler $handler,
    ) {
    }

    public function dependencies(): array
    {
        return [
            $this->service,
            $this->handler,
            new FallbackOnly(),
            new MappedThing(),
            new Report(),
            new \Legacy_Controller_Admin(),
        ];
    }
}
