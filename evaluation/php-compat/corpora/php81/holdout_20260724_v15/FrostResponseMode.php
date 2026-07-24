<?php

declare(strict_types=1);

namespace Fixture\PHP81\HoldoutV15;

enum FrostResponseMode: string
{
    case DeployCovers = 'deploy_covers';
    case StartIrrigation = 'start_irrigation';
    case IgniteHeaters = 'ignite_heaters';
    case StandDown = 'stand_down';
}
