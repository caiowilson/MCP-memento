<?php

declare(strict_types=1);

namespace SeedArchive\Domain;

enum SeedLotViability: string
{
    case Untested = 'untested';
    case GerminationQueued = 'germination_queued';
    case Viable = 'viable';
    case Retired = 'retired';
}
