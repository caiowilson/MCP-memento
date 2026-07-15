<?php
declare(strict_types=1);

namespace App\Http\Controllers\Seafoam;

use App\Models\Seafoam\Skiff;
use App\Services\Seafoam\BerthPlanner;

final class BerthAssignmentController
{
    public function __construct(private BerthPlanner $planner)
    {
    }

    public function __invoke(Skiff $skiff): array
    {
        return $this->planner->plan($skiff->harbor);
    }
}
