<?php

declare(strict_types=1);

namespace App\Repository;

use App\Entity\Report;
use Doctrine\Bundle\DoctrineBundle\Repository\ServiceEntityRepository;

final class ReportRepository extends ServiceEntityRepository
{
    public function findRecent(): array
    {
        return [Report::class];
    }
}
