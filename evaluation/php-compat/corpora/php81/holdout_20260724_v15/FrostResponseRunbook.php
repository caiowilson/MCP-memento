<?php

declare(strict_types=1);

namespace Fixture\PHP81\HoldoutV15;

final class FrostResponseRunbook
{
    public function instruction(FrostResponseMode $mode): string
    {
        return match ($mode) {
            FrostResponseMode::DeployCovers => 'Deploy orchard covers before ice forms.',
            FrostResponseMode::StartIrrigation => 'Start frost irrigation at the configured flow.',
            FrostResponseMode::IgniteHeaters => 'Ignite orchard heaters in the exposed rows.',
            FrostResponseMode::StandDown => 'Stand down the frost response crew.',
        };
    }
}
